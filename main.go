package main

//go:generate go run github.com/go-bindata/go-bindata/go-bindata -pkg $GOPACKAGE -o assets.go assets/

import (
	"bufio"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"os/user"
	"reflect"
	"sync"
	"syscall"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"github.com/apenwarr/fixconsole"
	"github.com/getlantern/systray"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	unixSocket  = flag.String("wsl", "", "Path to Unix socket for passthrough to WSL")
	namedPipe   = flag.String("winssh", "", "Named pipe for use with Win32 OpenSSH")
	verbose     = flag.Bool("verbose", false, "Enable verbose logging")
	systrayFlag = flag.Bool("systray", false, "Enable systray integration")
	force       = flag.Bool("force", false, "Force socket usage (unlink existing socket)")
)

const (
	// Windows constats
	invalidHandleValue = ^windows.Handle(0)
	pageReadWrite      = 0x4
	fileMapWrite       = 0x2

	// ssh-agent/Pageant constants
	agentMaxMessageLength = 8192
	agentCopyDataID       = 0x804e50ba
)

// copyDataStruct is used to pass data in the WM_COPYDATA message.
// We directly pass a pointer to our copyDataStruct type, we need to be
// careful that it matches the Windows type exactly
type copyDataStruct struct {
	dwData uintptr
	cbData uint32
	lpData uintptr
}

type SecurityAttributes struct {
	Length             uint32
	SecurityDescriptor uintptr
	InheritHandle      uint32
}

var queryPageantMutex sync.Mutex

func makeInheritSaWithSid() *windows.SecurityAttributes {
	var sa windows.SecurityAttributes

	u, err := user.Current()

	if err == nil {
		sd, err := windows.SecurityDescriptorFromString("O:" + u.Uid)
		if err == nil {
			sa.SecurityDescriptor = sd
		}
	}

	sa.Length = uint32(unsafe.Sizeof(sa))

	sa.InheritHandle = 1

	return &sa

}

func queryPageant_(buf []byte) (result []byte, err error) {
	if len(buf) > agentMaxMessageLength {
		err = errors.New("Message too long")
		return
	}

	hwnd := win.FindWindow(syscall.StringToUTF16Ptr("Pageant"), syscall.StringToUTF16Ptr("Pageant"))

	if hwnd == 0 {
		err = errors.New("Could not find Pageant window")
		return
	}

	// Typically you'd add thread ID here but thread ID isn't useful in Go
	// We would need goroutine ID but Go hides this and provides no good way of
	// accessing it, instead we serialise calls to queryPageant and treat it
	// as not being goroutine safe
	mapName := fmt.Sprintf("WSLPageantRequest")
	queryPageantMutex.Lock()

	var sa = makeInheritSaWithSid()

	fileMap, err := windows.CreateFileMapping(invalidHandleValue, sa, pageReadWrite, 0, agentMaxMessageLength, syscall.StringToUTF16Ptr(mapName))
	if err != nil {
		queryPageantMutex.Unlock()
		return
	}
	defer func() {
		windows.CloseHandle(fileMap)
		queryPageantMutex.Unlock()
	}()

	sharedMemory, err := windows.MapViewOfFile(fileMap, fileMapWrite, 0, 0, 0)
	if err != nil {
		return
	}
	defer windows.UnmapViewOfFile(sharedMemory)

	sharedMemoryArray := (*[agentMaxMessageLength]byte)(unsafe.Pointer(sharedMemory))
	copy(sharedMemoryArray[:], buf)

	mapNameWithNul := mapName + "\000"

	// We use our knowledge of Go strings to get the length and pointer to the
	// data and the length directly
	cds := copyDataStruct{
		dwData: agentCopyDataID,
		cbData: uint32(((*reflect.StringHeader)(unsafe.Pointer(&mapNameWithNul))).Len),
		lpData: ((*reflect.StringHeader)(unsafe.Pointer(&mapNameWithNul))).Data,
	}

	ret := win.SendMessage(hwnd, win.WM_COPYDATA, 0, uintptr(unsafe.Pointer(&cds)))
	if ret == 0 {
		err = errors.New("WM_COPYDATA failed")
		return
	}

	len := binary.BigEndian.Uint32(sharedMemoryArray[:4])
	len += 4

	if len > agentMaxMessageLength {
		err = errors.New("Return message too long")
		return
	}

	result = make([]byte, len)
	copy(result, sharedMemoryArray[:len])

	return
}

func queryAgent(pipeName string, buf []byte) (result []byte, err error) {
	if len(buf) > agentMaxMessageLength {
		err = errors.New("Message too long")
		return
	}

	debug := false
	conn, err := winio.DialPipe(pipeName, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to pipe %s: %w", pipeName, err)
	} else if debug {
		log.Printf("Connected to %s: %d", pipeName, len(buf))
	}
	defer conn.Close()

	l, err := conn.Write(buf)
	if err != nil {
		return nil, fmt.Errorf("cannot write to pipe %s: %w", pipeName, err)
	} else if debug {
		log.Printf("Sent to %s: %d", pipeName, l)
	}

	reader := bufio.NewReader(conn)
	res := make([]byte, agentMaxMessageLength)

	l, err = reader.Read(res)
	if err != nil {
		return nil, fmt.Errorf("cannot read from pipe %s: %w", pipeName, err)
	} else if debug {
		log.Printf("Received from %s: %d", pipeName, l)
	}
	return res[0:l], nil
}

var failureMessage = [...]byte{0, 0, 0, 1, 5}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	for {
		lenBuf := make([]byte, 4)
		_, err := io.ReadFull(reader, lenBuf)
		if err != nil {
			if *verbose {
				log.Printf("io.ReadFull error '%s'", err)
			}
			return
		}

		len := binary.BigEndian.Uint32(lenBuf)
		buf := make([]byte, len)
		_, err = io.ReadFull(reader, buf)
		if err != nil {
			if *verbose {
				log.Printf("io.ReadFull error '%s'", err)
			}
			return
		}

		result, err := queryPageant_(append(lenBuf, buf...))
		// result, err := queryAgent("\\\\.\\pipe\\openssh-ssh-agent", append(lenBuf, buf...))
		if err != nil {
			// If for some reason talking to Pageant fails we fall back to
			// sending an agent error to the client
			if *verbose {
				log.Printf("Pageant query error '%s'", err)
			}
			result = failureMessage[:]
		}

		_, err = conn.Write(result)
		if err != nil {
			if *verbose {
				log.Printf("net.Conn.Write error '%s'", err)
			}
			return
		}
	}
}

func listenLoop(ln net.Listener) {
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("net.Listener.Accept error '%s'", err)
			return
		}

		if *verbose {
			log.Printf("New connection: %v\n", conn)
		}

		go handleConnection(conn)
	}
}

func main_() {
	fixconsole.FixConsoleIfNeeded()
	flag.Parse()

	var unix, pipe net.Listener
	var err error

	done := make(chan bool, 1)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		switch sig {
		case os.Interrupt:
			log.Printf("Caught signal")
			done <- true
		}
	}()

	if *unixSocket != "" {
		if *force {
			// If the socket file already exists then unlink it
			_, err := os.Stat(*unixSocket)
			if err == nil || !os.IsNotExist(err) {
				err = syscall.Unlink(*unixSocket)
				if err != nil {
					log.Fatalf("Failed to unlink socket %s, error '%s'\n", *unixSocket, err)
				}
			}
		}

		unix, err = net.Listen("unix", *unixSocket)
		if err != nil {
			log.Fatalf("Could not open socket %s, error '%s'\n", *unixSocket, err)
		}

		defer unix.Close()
		log.Printf("Listening on Unix socket: %s\n", *unixSocket)
		go func() {
			listenLoop(unix)

			// If for some reason our listener breaks, kill the program
			done <- true
		}()
	}

	if *namedPipe != "" {
		namedPipeFullName := "\\\\.\\pipe\\" + *namedPipe
		var cfg = &winio.PipeConfig{}
		pipe, err = winio.ListenPipe(namedPipeFullName, cfg)
		if err != nil {
			log.Fatalf("Could not open named pipe %s, error '%s'\n", namedPipeFullName, err)
		}

		defer pipe.Close()
		log.Printf("Listening on named pipe: %s\n", namedPipeFullName)
		go func() {
			listenLoop(pipe)

			// If for some reason our listener breaks, kill the program
			done <- true
		}()
	}

	// pageantWindowClass := `\o/ Walk_Clipboard_Class \o/`
	// var wc win.WNDCLASSEX
	// wc.CbSize = uint32(unsafe.Sizeof(wc))
	// wc.LpfnWndProc = wndProcPtr
	// wc.HInstance = hInst
	// wc.HIcon = hIcon
	// wc.HCursor = hCursor
	// wc.HbrBackground = win.COLOR_BTNFACE + 1
	// wc.LpszClassName = syscall.StringToUTF16Ptr(className)

	// if atom := win.RegisterClassEx(&wc); atom == 0 {
	// 	panic("RegisterClassEx")
	// }

	// // MustRegisterWindowClass(pageantWindowClass)
	// // pageantWindow := win.CreateWindowEx(0, syscall.StringToUTF16Ptr("pageantss"), nil, win.WS_CAPTION, 0, 0, 0, 0, 0, 0, 0, nil)
	// pageantWindow := win.CreateWindowEx( 0,
	// 	syscall.StringToUTF16Ptr(pageantWindowClass),
	// 	nil,
	// 	0,
	// 	0,
	// 	0,
	// 	0,
	// 	0,
	// 	win.HWND_MESSAGE,
	// 	0,
	// 	0,
	// 	nil)
	// if pageantWindow == 0 {
	// 	log.Println("Couldn't create Pageant window.")
	// }

	// var msg win.MSG
	// // msg.Message = win.WM_QUIT + 1 // win.WM_QUIT

	// for win.GetMessage(&msg, pageantWindow, 0, 0) > 0 {
	// 	// win.TranslateMessage(&msg)
	// 	// win.DispatchMessage(&msg)
	// 	log.Println("Received putty message")
	// }

	if *namedPipe == "" && *unixSocket == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *systrayFlag {
		go func() {
			// Wait until we are signalled as finished
			<-done

			// If for some reason our listener breaks, kill the program
			systray.Quit()
		}()

		systray.Run(onSystrayReady, nil)

		log.Print("Exiting...")
	} else {
		// Wait until we are signalled as finished
		<-done

		log.Print("Exiting...")
	}
}

func onSystrayReady() {
	systray.SetTitle("WSL-SSH-Pageant")
	systray.SetTooltip("WSL-SSH-Pageant")

	data, err := Asset("assets/icon.ico")
	if err == nil {
		systray.SetIcon(data)
	}

	quit := systray.AddMenuItem("Quit", "Quits this app")

	go func() {
		for {
			select {
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}
