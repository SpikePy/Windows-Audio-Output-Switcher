//go:build windows

package updater

import (
	"fmt"
	"io"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A minimal HTTPS client on WinHTTP, Windows' built-in HTTP stack, used
// instead of net/http: setup makes only two requests, and Go's own
// HTTP/TLS implementation more than doubled its size. WinHTTP also honors
// the system's proxy settings.
//
// winhttp.dll isn't one of Windows' KnownDLLs, so it's loaded strictly
// from System32 - never from the directory setup runs from (typically
// Downloads), where a planted DLL of the same name would otherwise win.
var (
	winhttp                    = windows.NewLazySystemDLL("winhttp.dll")
	procWinHttpOpen            = winhttp.NewProc("WinHttpOpen")
	procWinHttpConnect         = winhttp.NewProc("WinHttpConnect")
	procWinHttpOpenRequest     = winhttp.NewProc("WinHttpOpenRequest")
	procWinHttpSetTimeouts     = winhttp.NewProc("WinHttpSetTimeouts")
	procWinHttpSetOption       = winhttp.NewProc("WinHttpSetOption")
	procWinHttpSendRequest     = winhttp.NewProc("WinHttpSendRequest")
	procWinHttpReceiveResponse = winhttp.NewProc("WinHttpReceiveResponse")
	procWinHttpQueryHeaders    = winhttp.NewProc("WinHttpQueryHeaders")
	procWinHttpReadData        = winhttp.NewProc("WinHttpReadData")
	procWinHttpCloseHandle     = winhttp.NewProc("WinHttpCloseHandle")
)

const (
	userAgent = "AudioOutputSwitcher-Installer"
	timeoutMs = 30_000 // per resolve/connect/send/receive step

	accessTypeAutomaticProxy = 4 // WINHTTP_ACCESS_TYPE_AUTOMATIC_PROXY
	httpsPort                = 443
	flagSecure               = 0x00800000
	optionRedirectPolicy     = 88
	redirectPolicyNever      = 0
	queryStatusCode          = 19
	queryLocation            = 33
	queryFlagNumber          = 0x20000000
)

// request is an open WinHTTP request whose response headers have arrived.
type request struct {
	session, connection, handle uintptr
}

func (r *request) close() {
	for _, h := range []uintptr{r.handle, r.connection, r.session} {
		if h != 0 {
			procWinHttpCloseHandle.Call(h)
		}
	}
}

// splitHTTPSURL splits an https:// URL into its host and path.
func splitHTTPSURL(rawURL string) (host, path string, err error) {
	rest, ok := strings.CutPrefix(rawURL, "https://")
	if !ok {
		return "", "", fmt.Errorf("not an https URL: %q", rawURL)
	}
	host, path, _ = strings.Cut(rest, "/")
	return host, "/" + path, nil
}

// send issues an HTTPS request and waits for its response headers. With
// followRedirects false, a redirect response is returned as-is instead of
// being followed.
func send(method, rawURL string, followRedirects bool) (*request, error) {
	host, path, err := splitHTTPSURL(rawURL)
	if err != nil {
		return nil, err
	}

	r := &request{}
	succeeded := false
	defer func() {
		if !succeeded {
			r.close()
		}
	}()

	agent, _ := windows.UTF16PtrFromString(userAgent)
	r.session, _, err = procWinHttpOpen.Call(uintptr(unsafe.Pointer(agent)), accessTypeAutomaticProxy, 0, 0, 0)
	if r.session == 0 {
		return nil, fmt.Errorf("WinHttpOpen: %w", err)
	}
	procWinHttpSetTimeouts.Call(r.session, timeoutMs, timeoutMs, timeoutMs, timeoutMs)

	hostPtr, _ := windows.UTF16PtrFromString(host)
	r.connection, _, err = procWinHttpConnect.Call(r.session, uintptr(unsafe.Pointer(hostPtr)), httpsPort, 0)
	if r.connection == 0 {
		return nil, fmt.Errorf("connect to %s: %w", host, err)
	}

	verbPtr, _ := windows.UTF16PtrFromString(method)
	pathPtr, _ := windows.UTF16PtrFromString(path)
	r.handle, _, err = procWinHttpOpenRequest.Call(r.connection,
		uintptr(unsafe.Pointer(verbPtr)), uintptr(unsafe.Pointer(pathPtr)), 0, 0, 0, flagSecure)
	if r.handle == 0 {
		return nil, fmt.Errorf("open request: %w", err)
	}

	if !followRedirects {
		policy := uint32(redirectPolicyNever)
		if ret, _, err := procWinHttpSetOption.Call(r.handle, optionRedirectPolicy,
			uintptr(unsafe.Pointer(&policy)), unsafe.Sizeof(policy)); ret == 0 {
			return nil, fmt.Errorf("disable redirects: %w", err)
		}
	}

	if ret, _, err := procWinHttpSendRequest.Call(r.handle, 0, 0, 0, 0, 0, 0); ret == 0 {
		return nil, fmt.Errorf("send request: %w", err)
	}
	if ret, _, err := procWinHttpReceiveResponse.Call(r.handle, 0); ret == 0 {
		return nil, fmt.Errorf("receive response: %w", err)
	}
	succeeded = true
	return r, nil
}

func (r *request) statusCode() (int, error) {
	var code, size uint32 = 0, 4
	if ret, _, err := procWinHttpQueryHeaders.Call(r.handle, queryStatusCode|queryFlagNumber, 0,
		uintptr(unsafe.Pointer(&code)), uintptr(unsafe.Pointer(&size)), 0); ret == 0 {
		return 0, fmt.Errorf("read status code: %w", err)
	}
	return int(code), nil
}

// location returns the response's Location header, or "" if it has none.
func (r *request) location() string {
	// A first call without a buffer fails but reports the size needed.
	var size uint32
	procWinHttpQueryHeaders.Call(r.handle, queryLocation, 0, 0, uintptr(unsafe.Pointer(&size)), 0)
	if size == 0 {
		return ""
	}
	buf := make([]uint16, size/2+1)
	if ret, _, _ := procWinHttpQueryHeaders.Call(r.handle, queryLocation, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0); ret == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// redirectLocation requests rawURL without following redirects and
// returns the response's status code and Location header.
func redirectLocation(rawURL string) (status int, location string, err error) {
	r, err := send("HEAD", rawURL, false)
	if err != nil {
		return 0, "", err
	}
	defer r.close()

	if status, err = r.statusCode(); err != nil {
		return 0, "", err
	}
	return status, r.location(), nil
}

// download GETs rawURL, following redirects, and writes the body to w.
func download(rawURL string, w io.Writer) error {
	r, err := send("GET", rawURL, true)
	if err != nil {
		return err
	}
	defer r.close()

	status, err := r.statusCode()
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("unexpected status %d", status)
	}

	buf := make([]byte, 64*1024)
	for {
		var n uint32
		if ret, _, err := procWinHttpReadData.Call(r.handle,
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&n))); ret == 0 {
			return fmt.Errorf("read response: %w", err)
		}
		if n == 0 {
			return nil
		}
		if _, err := w.Write(buf[:n]); err != nil {
			return err
		}
	}
}
