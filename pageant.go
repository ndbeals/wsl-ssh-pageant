package main

import (
	"fmt"
	"reflect"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
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

func WndProc(hWnd win.HWND, message uint32, wParam uintptr, lParam uintptr) uintptr {
	switch message {
	case win.WM_COPYDATA:
	{
		// cds := unsafe.Pointer(lParam)
		cds := (*copyDataStruct)(unsafe.Pointer(lParam))
		
		fmt.Printf("wndProc COPYDATA: %+v %+v %+v \n", hWnd, wParam, lParam)

		nameLength := cds.cbData//(*reflect.StringHeader)(unsafe.Pointer(uintptr(cds.cbData)))
		name := (*reflect.StringHeader)(unsafe.Pointer(uintptr(cds.lpData)))

		// copyDataStruct *fs
		// fs = copyDataStruct (&cds)

		fmt.Printf("cds: %+v %+v %+v %+v",nameLength,name)
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