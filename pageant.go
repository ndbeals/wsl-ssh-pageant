package main

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	wndClassName = "Pageant"
)

func main() {
	inst := win.GetModuleHandle(nil)
	WinMain(inst)
}

func WinMain(Inst win.HINSTANCE) int32 {
	// RegisterClass
	atom := MyRegisterClass(Inst)
	if atom == 0 {
		fmt.Println("RegisterClass failed:", win.GetLastError())
		return 0
	}
	fmt.Println("RegisterClass ok", atom)

	// CreateWindowEx
	wnd := win.CreateWindowEx(win.WS_EX_APPWINDOW,
		syscall.StringToUTF16Ptr(wndClassName),
		syscall.StringToUTF16Ptr(wndClassName),
		0,
		0, 0,
        0, 0,
		0,
		0,
		Inst,
		nil)
	if wnd == 0 {
		fmt.Println("CreateWindowEx failed:", win.GetLastError())
		return 0
	}
	fmt.Println("CreateWindowEx done", wnd)
	// win.ShowWindow(wnd, win.SW_SHOW)
	// win.UpdateWindow(wnd)

	// main message loop
	var msg win.MSG
	for win.GetMessage(&msg, 0,0,0) > 0 {
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}

	return int32(msg.WParam)
}


func utf16PtrToString(p *byte, max uint32) string {

	if p == nil {

		return ""

	}

	// Find NUL terminator.

	end := unsafe.Pointer(p)

	var n uint32 = 0

	for *(*byte)(end) != 0 && n < max {

		end = unsafe.Pointer(uintptr(end) + unsafe.Sizeof(*p))

		n++

	}

	s := (*[(1 << 30) - 1]byte)(unsafe.Pointer(p))[:n:n]

	return string(s)

}
var (
	modkernel32          = syscall.NewLazyDLL("kernel32.dll")
	procOpenFileMappingW                 = modkernel32.NewProc("OpenFileMappingW")
)


// StringToCharPtr converts a Go string into pointer to a null-terminated cstring.
// This assumes the go string is already ANSI encoded.
func StringToCharPtr(str string) *uint8 {
	chars := append([]byte(str), 0) // null terminated
	return &chars[0]
}

func WndProc(hWnd win.HWND, message uint32, wParam uintptr, lParam uintptr) uintptr {
	switch message {
	case win.WM_COPYDATA:
	{
		// cds := unsafe.Pointer(lParam)
		cds := (*copyDataStruct)(unsafe.Pointer(lParam))
		
		fmt.Printf("wndProc COPYDATA: %+v %+v %+v \n", hWnd, wParam, lParam)

		nameLength := cds.cbData//(*reflect.StringHeader)(unsafe.Pointer(uintptr(cds.cbData)))
		// name := (*reflect.StringHeader)(unsafe.Pointer(uintptr(cds.lpData)))
		// name := (*string)(unsafe.Pointer(cds.lpData))
		// name := (*char)(unsafe.Pointer(cds.lpData))
		// name := make([]uint16,nameLength)
		var data *byte = (*byte)(unsafe.Pointer(cds.lpData))
		// mapName := fmt.Sprintf( utf16PtrToString(data,nameLength) )
		mapName := utf16PtrToString(data,nameLength)
		// sa := makeInheritSaWithSid()
		
		// fileMap, err := windows.CreateFileMapping(invalidHandleValue, sa, pageReadWrite, 0, agentMaxMessageLength, syscall.StringToUTF16Ptr(mapName))
		// fileMap,err := syscall.Open(mapName,0,0)
		// fileMap,_,err := syscall.Syscall(procOpenFileMappingW.Addr(),3,4,1,uintptr(*syscall.StringToUTF16Ptr(mapName)))
		fileMapp,_,err := procOpenFileMappingW.Call(uintptr(4),uintptr(1),uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(mapName))))
		fmt.Printf("ecists: %d \n",win.GetLastError())
		// if err != nil {
			// 	fmt.Errorf("shit. %d \n",win.GetLastError())
		// }
		fileMap := (windows.Handle)(unsafe.Pointer(fileMapp))
		defer func() {
			windows.CloseHandle(fileMap)
		// 	// queryPageantMutex.Unlock()
		}()
		if err != syscall.Errno(0) {
			fmt.Errorf("Error on open, \n")
			return 0
		}
		
		// // const unt
		sharedMemory, err := windows.MapViewOfFile(fileMap, 4, 0, 0, 0)
		defer windows.UnmapViewOfFile(sharedMemory)

		sharedMemoryArray := (*[agentMaxMessageLength]byte)(unsafe.Pointer(sharedMemory))
		// // var buf []byte //= make([]byte)
		// // copy(sharedMemoryArray[:], buf)
		// // copy(buf, sharedMemoryArray[:])
		// // lenBuf := len(buf)
		lenBuf := sharedMemoryArray[0:4]
		leng := binary.BigEndian.Uint32(lenBuf)
		bb := sharedMemoryArray[200:210]
		// leng :=0
		// bb:=0
		
		// result, err := queryAgent("\\\\.\\pipe\\openssh-ssh-agent", append(lenBuf, buf...))
		// copyDataStruct *fs
		// fs = copyDataStruct (&cds)

		fmt.Printf("cds: %+v %+v %+v %+v %+v %+v",mapName, fileMap,leng,bb)
		// cds := copyDataStruct{lParam}
		return 1;
	}	
	}
	// ret, handled := gohl.ProcNoDefault(hWnd, message, wParam, lParam)
	// if handled {
	// 	return uintptr(ret)
	// }
	// switch message {
	// case win.WM_CREATE:
	// 	println("win.WM_CREATE called", win.WM_CREATE)
	// }
	return win.DefWindowProc(hWnd, message, wParam, lParam)
}

func MyRegisterClass(hInstance win.HINSTANCE) (atom win.ATOM) {
	var wc win.WNDCLASSEX
	wc.Style = 0
	

	wc.CbSize = uint32(unsafe.Sizeof(wc))
	// wc.Style = win.CS_HREDRAW | win.CS_VREDRAW
	wc.LpfnWndProc = syscall.NewCallback(WndProc)
	wc.CbClsExtra = 0
	wc.CbWndExtra = 0
	wc.HInstance = hInstance
	wc.HIcon = win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_APPLICATION))
	wc.HCursor = win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_IBEAM))
	wc.HbrBackground = win.GetSysColorBrush(win.BLACK_BRUSH)
	wc.LpszMenuName = nil // syscall.StringToUTF16Ptr("")
	wc.LpszClassName = syscall.StringToUTF16Ptr(wndClassName)
	wc.HIconSm = win.LoadIcon(0, win.MAKEINTRESOURCE(win.IDI_APPLICATION))

	return win.RegisterClassEx(&wc)
}

func init() {
	// runtime.GOMAXPROCS(runtime.NumCPU())
	runtime.GOMAXPROCS(1)
}