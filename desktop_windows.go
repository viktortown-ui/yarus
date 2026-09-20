//go:build windows

package main

// A small native Win32 host for the installed Microsoft WebView2 Runtime.
// No external browser, Node integration, remote executable code, or embedded server keys.
// The official Microsoft WebView2Loader.dll is shipped beside YARUS.exe.
import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var winUser = syscall.NewLazyDLL("user32.dll")
var winKernel = syscall.NewLazyDLL("kernel32.dll")
var winOle = syscall.NewLazyDLL("ole32.dll")
var winDialog = syscall.NewLazyDLL("comdlg32.dll")
var winShell = syscall.NewLazyDLL("shell32.dll")

func wp(d *syscall.LazyDLL, n string) *syscall.LazyProc { return d.NewProc(n) }
func wstr(s string) *uint16                             { p, _ := syscall.UTF16PtrFromString(s); return p }
func nativeAlert(message string) {
	wp(winUser, "MessageBoxW").Call(0, uintptr(unsafe.Pointer(wstr(message))), uintptr(unsafe.Pointer(wstr("ЯРУС"))), 0x10)
}

type winRect struct{ Left, Top, Right, Bottom int32 }
type winPoint struct{ X, Y int32 }
type winMsg struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          winPoint
	Private        uint32
}
type winClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	SmallIcon                          uintptr
}
type winGUID struct {
	A    uint32
	B, C uint16
	D    [8]byte
}

func guid(text string) winGUID {
	var g winGUID
	wp(winOle, "CLSIDFromString").Call(uintptr(unsafe.Pointer(wstr("{"+text+"}"))), uintptr(unsafe.Pointer(&g)))
	return g
}
func callCOM(object uintptr, index int, args ...uintptr) uintptr {
	if object == 0 {
		return 0x80004003
	}
	vtable := *(*uintptr)(unsafe.Pointer(object))
	fn := *(*uintptr)(unsafe.Pointer(vtable + uintptr(index)*unsafe.Sizeof(uintptr(0))))
	a := append([]uintptr{object}, args...)
	r, _, _ := syscall.SyscallN(fn, a...)
	return r
}
func comText(object uintptr, index int) string {
	var p *uint16
	if int32(callCOM(object, index, uintptr(unsafe.Pointer(&p)))) < 0 || p == nil {
		return ""
	}
	defer wp(winOle, "CoTaskMemFree").Call(uintptr(unsafe.Pointer(p)))
	var chars []uint16
	for i := uintptr(0); i < 64*1024*1024; i++ {
		c := *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + i*2))
		if c == 0 {
			break
		}
		chars = append(chars, c)
	}
	return syscall.UTF16ToString(chars)
}

type winCallback struct {
	VTable *[4]uintptr
	IID    winGUID
	Fn     func(uintptr, uintptr) uintptr
}

var winCallbacks []*winCallback
var winPin runtime.Pinner

func callback(iid string, fn func(uintptr, uintptr) uintptr) uintptr {
	c := &winCallback{IID: guid(iid), Fn: fn}
	table := &[4]uintptr{}
	table[0] = syscall.NewCallback(func(this, riid, out uintptr) uintptr {
		if out == 0 {
			return 0x80004003
		}
		*(*uintptr)(unsafe.Pointer(out)) = 0
		if riid == 0 {
			return 0x80004002
		}
		actual := *(*winGUID)(unsafe.Pointer(riid))
		obj := (*winCallback)(unsafe.Pointer(this))
		if actual == obj.IID || actual == guid("00000000-0000-0000-C000-000000000046") {
			*(*uintptr)(unsafe.Pointer(out)) = this
			return 0
		}
		return 0x80004002
	})
	table[1] = syscall.NewCallback(func(this uintptr) uintptr { return 2 })
	table[2] = syscall.NewCallback(func(this uintptr) uintptr { return 1 })
	table[3] = syscall.NewCallback(func(this, a, b uintptr) (result uintptr) {
		defer func() {
			if r := recover(); r != nil {
				winFailure = fmt.Errorf("WebView2 callback: %v", r)
				wp(winUser, "PostMessageW").Call(winWindow, 0x10, 0, 0)
				result = 0x80004005
			}
		}()
		return (*winCallback)(unsafe.Pointer(this)).Fn(a, b)
	})
	c.VTable = table
	winCallbacks = append(winCallbacks, c)
	winPin.Pin(c)
	winPin.Pin(table)
	return uintptr(unsafe.Pointer(c))
}

var winWindow, winController, winWeb, winEnvironment uintptr
var winFailure error
var winAllowed string
var pendingNativeMessages []nativeMessage
var winEnvironmentDLL *syscall.DLL

type nativeMessage struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Text  string `json:"text"`
	Mime  string `json:"mime"`
	Value string `json:"value"`
}

func nativeTrusted(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "http" && u.Host == winAllowed && u.User == nil
}
func nativeToast(text string) {
	encoded, _ := json.Marshal(text)
	js := "if(typeof toast==='function')toast(" + string(encoded) + ");"
	callCOM(winWeb, 29, uintptr(unsafe.Pointer(wstr(js))), 0)
}
func fitWebview() {
	if winController == 0 {
		return
	}
	var rect winRect
	wp(winUser, "GetClientRect").Call(winWindow, uintptr(unsafe.Pointer(&rect)))
	callCOM(winController, 6, uintptr(unsafe.Pointer(&rect)))
}
func winProcedure(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case 5:
		fitWebview()
		return 0
	case 7:
		if winController != 0 {
			callCOM(winController, 12, 0)
		}
		return 0
	case 0x113: // WM_TIMER: report startup failure instead of a permanently blank window.
		if wparam == 1 && winWeb == 0 {
			winFailure = errors.New("Окно WebView2 не запустилось за 30 секунд. Обновите WebView2 Runtime и повторите запуск. Данные не удалены.")
			wp(winUser, "PostMessageW").Call(hwnd, 0x10, 0, 0)
		}
		wp(winUser, "KillTimer").Call(hwnd, 1)
		return 0
	case 0x10:
		// Stop camera before destroying the view. Committed records and queued drafts are already persistent.
		if winWeb != 0 {
			callCOM(winWeb, 29, uintptr(unsafe.Pointer(wstr("if(typeof pauseScanner==='function')pauseScanner();"))), 0)
		}
		if winController != 0 {
			callCOM(winController, 24)
			callCOM(winController, 2)
			winController = 0
		}
		wp(winUser, "DestroyWindow").Call(hwnd)
		return 0
	case 2:
		wp(winUser, "PostQuitMessage").Call(0)
		return 0
	case 0x8001:
		if len(pendingNativeMessages) > 0 {
			m := pendingNativeMessages[0]
			pendingNativeMessages = pendingNativeMessages[1:]
			if e := saveNativeFile(hwnd, m); e != nil {
				nativeToast(e.Error())
			}
		}
		return 0
	}
	r, _, _ := wp(winUser, "DefWindowProcW").Call(hwnd, uintptr(msg), wparam, lparam)
	return r
}

func findLoaderDLL() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(executable), "WebView2Loader.dll")
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		return path, nil
	}
	return "", errors.New("Рядом с YARUS.exe нет WebView2Loader.dll. Полностью распакуйте архив ЯРУС и запустите приложение снова. Данные не удалены")
}

func desktopUI(address string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	winFailure = nil
	winWindow = 0
	winController = 0
	winWeb = 0
	winEnvironment = 0
	u, err := url.Parse(address)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Scheme != "http" {
		return errors.New("Недопустимый локальный адрес окна ЯРУС")
	}
	winAllowed = u.Host
	hr, _, _ := wp(winOle, "CoInitializeEx").Call(0, 2)
	if int32(hr) < 0 {
		return fmt.Errorf("COM initialization 0x%x", hr)
	}
	defer wp(winOle, "CoUninitialize").Call()
	dpi := wp(winUser, "SetProcessDpiAwarenessContext")
	if dpi.Find() == nil {
		dpi.Call(^uintptr(3))
	}
	dllPath, err := findLoaderDLL()
	if err != nil {
		return err
	}
	winEnvironmentDLL, err = syscall.LoadDLL(dllPath)
	if err != nil {
		return fmt.Errorf("WebView2: %w", err)
	}
	create, err := winEnvironmentDLL.FindProc("CreateCoreWebView2EnvironmentWithOptions")
	if err != nil {
		return fmt.Errorf("Компонент WebView2 несовместим. Обновите Evergreen Runtime. %w", err)
	}
	profile := filepath.Join(os.Getenv("LOCALAPPDATA"), "Yarus", "desktop-webview")
	if executable, executableErr := os.Executable(); executableErr == nil {
		root := filepath.Dir(executable)
		if _, markerErr := os.Stat(filepath.Join(root, "YARUS-PORTABLE.flag")); markerErr == nil {
			profile = filepath.Join(root, "data", "desktop-webview")
		}
	}
	if err = os.MkdirAll(profile, 0700); err != nil {
		return err
	}
	instance, _, _ := wp(winKernel, "GetModuleHandleW").Call(0)
	cursor, _, _ := wp(winUser, "LoadCursorW").Call(0, 32512)
	icon, _, _ := wp(winUser, "LoadIconW").Call(instance, 1)
	class := winClass{Size: uint32(unsafe.Sizeof(winClass{})), Style: 3, Proc: syscall.NewCallback(winProcedure), Instance: instance, Cursor: cursor, Icon: icon, SmallIcon: icon, Background: 6, Name: wstr("YarusDesktop100")}
	if r, _, e := wp(winUser, "RegisterClassExW").Call(uintptr(unsafe.Pointer(&class))); r == 0 {
		return fmt.Errorf("Окно ЯРУС: %w", e)
	}
	winWindow, _, err = wp(winUser, "CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class.Name)), uintptr(unsafe.Pointer(wstr("ЯРУС · ваш склад в порядке"))), 0x00cf0000, 0x80000000, 0x80000000, 1240, 840, 0, 0, instance, 0)
	if winWindow == 0 {
		return err
	}
	wp(winUser, "ShowWindow").Call(winWindow, 5)
	wp(winUser, "UpdateWindow").Call(winWindow)
	wp(winUser, "SetTimer").Call(winWindow, 1, 30000, 0)
	fail := func(text string) {
		winFailure = errors.New(text)
		wp(winUser, "PostMessageW").Call(winWindow, 0x10, 0, 0)
	}
	envCallback := callback("4e8a3389-c9d8-4bd2-b6b5-124fee6cc14d", func(result, environment uintptr) uintptr {
		if int32(result) < 0 || environment == 0 {
			fail(fmt.Sprintf("Не удалось создать WebView2: 0x%x", result))
			return 0
		}
		iid := guid("b96d755e-0319-4e92-a296-23436f46a1fc")
		if h := callCOM(environment, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&winEnvironment))); int32(h) < 0 {
			fail("WebView2 environment interface unavailable")
			return 0
		}
		controllerCallback := callback("6c4819f3-c9b7-4260-8127-c9f5bde7f68c", func(result, controller uintptr) uintptr {
			if int32(result) < 0 || controller == 0 {
				fail(fmt.Sprintf("Не удалось открыть интерфейс ЯРУС: 0x%x", result))
				return 0
			}
			winController = controller
			callCOM(controller, 1)
			if h := callCOM(controller, 25, uintptr(unsafe.Pointer(&winWeb))); int32(h) < 0 {
				fail("Не удалось получить WebView2")
				return 0
			}
			callCOM(controller, 4, 1)
			fitWebview()
			var token int64
			navigation := callback("9adbe429-f36d-432b-9ddc-f8881fbd76e3", func(sender, args uintptr) uintptr {
				if !nativeTrusted(comText(args, 3)) {
					callCOM(args, 8, 1)
				}
				return 0
			})
			callCOM(winWeb, 7, navigation, uintptr(unsafe.Pointer(&token)))
			permission := callback("15e1c6a3-c72a-4df3-91d7-d097fbec6bfd", func(sender, args uintptr) uintptr {
				var kind int32
				callCOM(args, 4, uintptr(unsafe.Pointer(&kind)))
				allow := uintptr(2)
				if kind == 2 && nativeTrusted(comText(args, 3)) {
					allow = 1
				}
				callCOM(args, 7, allow)
				return 0
			})
			callCOM(winWeb, 23, permission, uintptr(unsafe.Pointer(&token)))
			message := callback("57213f19-00e6-49fa-8e07-898ea01ecbd2", func(sender, args uintptr) uintptr {
				if !nativeTrusted(comText(args, 3)) {
					return 0
				}
				raw := comText(args, 4)
				if len(raw) > 64<<20 {
					return 0
				}
				var m nativeMessage
				if json.Unmarshal([]byte(raw), &m) != nil {
					return 0
				}
				if m.Kind == "openExternal" {
					if validExternal(m.Value) {
						wp(winShell, "ShellExecuteW").Call(0, uintptr(unsafe.Pointer(wstr("open"))), uintptr(unsafe.Pointer(wstr(m.Value))), 0, 0, 1)
					}
					return 0
				}
				if m.Kind != "saveText" || len(pendingNativeMessages) > 0 {
					return 0
				}
				if !validExport(m) {
					return 0
				}
				pendingNativeMessages = append(pendingNativeMessages, m)
				wp(winUser, "PostMessageW").Call(winWindow, 0x8001, 0, 0)
				return 0
			})
			callCOM(winWeb, 34, message, uintptr(unsafe.Pointer(&token)))
			if h := callCOM(winWeb, 5, uintptr(unsafe.Pointer(wstr(address)))); int32(h) < 0 {
				fail("Не удалось открыть локальный интерфейс")
			}
			return 0
		})
		if h := callCOM(winEnvironment, 3, winWindow, controllerCallback); int32(h) < 0 {
			fail(fmt.Sprintf("WebView2 controller: 0x%x", h))
		}
		return 0
	})
	// Keep Runtime settings deterministic; do not accept executable/debugger overrides.
	for _, key := range []string{"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "WEBVIEW2_PIPE_FOR_SCRIPT_DEBUGGER", "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER", "WEBVIEW2_USER_DATA_FOLDER"} {
		_ = os.Unsetenv(key)
	}
	hr, _, _ = create.Call(0, uintptr(unsafe.Pointer(wstr(profile))), environmentOptions(), envCallback)
	if int32(hr) < 0 {
		wp(winUser, "DestroyWindow").Call(winWindow)
		return fmt.Errorf("Запуск WebView2: 0x%x. Обновите WebView2 Runtime.", hr)
	}
	var msg winMsg
	for {
		r, _, _ := wp(winUser, "GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		wp(winUser, "TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
		wp(winUser, "DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
	}
	if winWeb != 0 {
		callCOM(winWeb, 2)
		winWeb = 0
	}
	if winEnvironment != 0 {
		callCOM(winEnvironment, 2)
		winEnvironment = 0
	}
	// Callback objects stay pinned for process lifetime: the runtime may still release its handlers.
	return winFailure
}

func validExport(m nativeMessage) bool {
	allowed := map[string]string{"application/pdf": ".pdf", "application/json": ".json", "application/vnd.yarus+json": ".yarus", "image/svg+xml": ".svg", "text/csv": ".csv"}
	ext, ok := allowed[m.Mime]
	return ok && strings.HasSuffix(strings.ToLower(m.Name), ext) && len(m.Name) < 160 && len(m.Text) <= 48<<20 && !strings.ContainsAny(m.Name, "\\/:*?\"<>|\x00")
}

func validExternal(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "mailto" {
		return strings.EqualFold(u.Opaque, "fixerkrk@yandex.ru")
	}
	return u.Scheme == "https" && strings.EqualFold(u.Host, "t.me") && u.Path == "/bertosh1"
}

type saveDialog struct {
	Size                        uint32
	Owner, Instance             uintptr
	Filter, CustomFilter        *uint16
	MaxCustom, FilterIndex      uint32
	File                        *uint16
	MaxFile                     uint32
	FileTitle                   *uint16
	MaxFileTitle                uint32
	InitialDir, Title           *uint16
	Flags                       uint32
	FileOffset, ExtensionOffset uint16
	DefaultExt                  *uint16
	CustomData, Hook            uintptr
	TemplateName                *uint16
	Reserved                    uintptr
	Reserved1, FlagsEx          uint32
}

func saveNativeFile(hwnd uintptr, m nativeMessage) error {
	if !validExport(m) {
		return errors.New("Неподдерживаемый экспорт")
	}
	filename := make([]uint16, 32768)
	copy(filename, syscall.StringToUTF16(m.Name))
	ext := strings.TrimPrefix(filepath.Ext(m.Name), ".")
	// Double NUL filter, created explicitly (UTF16PtrFromString rejects embedded NUL).
	filter := []uint16{}
	for _, part := range []string{"Файл ЯРУС (*." + ext + ")", "*." + ext, "Все файлы", "*.*", ""} {
		filter = append(filter, syscall.StringToUTF16(part)...)
	}
	dlg := saveDialog{Size: uint32(unsafe.Sizeof(saveDialog{})), Owner: hwnd, Filter: &filter[0], FilterIndex: 1, File: &filename[0], MaxFile: uint32(len(filename)), Title: wstr("ЯРУС — сохранить файл"), Flags: 0x80000 | 0x800 | 0x8 | 0x2, DefaultExt: wstr(ext)}
	if ok, _, _ := wp(winDialog, "GetSaveFileNameW").Call(uintptr(unsafe.Pointer(&dlg))); ok == 0 {
		nativeToast("Сохранение отменено.")
		return nil
	}
	dest := syscall.UTF16ToString(filename)
	file, err := os.CreateTemp(filepath.Dir(dest), ".yarus-export-")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	_, err = file.WriteString(m.Text)
	if err == nil {
		err = file.Sync()
	}
	ce := file.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		err = os.Rename(temp, dest)
	}
	if err != nil {
		return fmt.Errorf("Не удалось сохранить файл: %w", err)
	}
	nativeToast("Файл сохранён.")
	return nil
}

// Minimal documented ICoreWebView2EnvironmentOptions. Optional newer interfaces
// correctly return E_NOINTERFACE; Runtime uses their default settings.
type winOptions struct{ VTable *[11]uintptr }

var winOptionsKeep *winOptions

func environmentOptions() uintptr {
	table := &[11]uintptr{}
	table[0] = syscall.NewCallback(func(this, riid, out uintptr) uintptr {
		if out == 0 {
			return 0x80004003
		}
		*(*uintptr)(unsafe.Pointer(out)) = 0
		if riid == 0 {
			return 0x80004002
		}
		g := *(*winGUID)(unsafe.Pointer(riid))
		if g == guid("2fde08a8-1e9a-4766-8c05-95a9ceb9d1c5") || g == guid("00000000-0000-0000-C000-000000000046") {
			*(*uintptr)(unsafe.Pointer(out)) = this
			return 0
		}
		return 0x80004002
	})
	table[1] = syscall.NewCallback(func(this uintptr) uintptr { return 2 })
	table[2] = syscall.NewCallback(func(this uintptr) uintptr { return 1 })
	getter := func(text string) uintptr {
		return syscall.NewCallback(func(this, out uintptr) uintptr {
			if out == 0 {
				return 0x80004003
			}
			chars := syscall.StringToUTF16(text)
			p, _, _ := wp(winOle, "CoTaskMemAlloc").Call(uintptr(len(chars) * 2))
			if p == 0 {
				return 0x8007000e
			}
			copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(chars)), chars)
			*(*uintptr)(unsafe.Pointer(out)) = p
			return 0
		})
	}
	table[3] = getter("")
	table[5] = getter("ru-RU")
	table[7] = getter("86.0.616.0")
	table[9] = syscall.NewCallback(func(this, out uintptr) uintptr {
		if out == 0 {
			return 0x80004003
		}
		*(*int32)(unsafe.Pointer(out)) = 0
		return 0
	})
	for _, index := range []int{4, 6, 8, 10} {
		table[index] = syscall.NewCallback(func(this, value uintptr) uintptr { return 1 })
	}
	winOptionsKeep = &winOptions{table}
	winPin.Pin(table)
	winPin.Pin(winOptionsKeep)
	return uintptr(unsafe.Pointer(winOptionsKeep))
}
