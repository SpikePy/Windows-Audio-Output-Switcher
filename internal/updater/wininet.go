//go:build windows

package updater

import (
	"fmt"
	"io"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Downloads go through WinINet, Windows' own HTTP stack, rather than Go's
// net/http: that keeps several MB of TLS code out of the exe, and it uses
// the user's system proxy settings and Windows' certificate store.
var (
	modWininet = windows.NewLazySystemDLL("wininet.dll")

	procInternetOpenW       = modWininet.NewProc("InternetOpenW")
	procInternetSetOptionW  = modWininet.NewProc("InternetSetOptionW")
	procInternetOpenUrlW    = modWininet.NewProc("InternetOpenUrlW")
	procHttpQueryInfoW      = modWininet.NewProc("HttpQueryInfoW")
	procInternetReadFile    = modWininet.NewProc("InternetReadFile")
	procInternetCloseHandle = modWininet.NewProc("InternetCloseHandle")
)

const (
	internetOpenTypePreconfig = 0 // use the system's proxy configuration

	internetFlagReload         = 0x80000000 // always ask the server, never the cache
	internetFlagNoCacheWrite   = 0x04000000
	internetFlagNoAutoRedirect = 0x00200000
	internetFlagNoCookies      = 0x00080000
	internetFlagNoUI           = 0x00000200

	internetOptionConnectTimeout = 2
	internetOptionReceiveTimeout = 6

	httpQueryStatusCode = 19
	httpQueryLocation   = 33
	httpQueryFlagNumber = 0x20000000

	// httpTimeoutMs bounds connecting and each individual read, so a
	// stalled connection fails instead of hanging, while a slow but
	// progressing download is never cut off.
	httpTimeoutMs uint32 = 30_000
)

// request is an open WinINet request, with the session it belongs to.
type request struct{ inet, req uintptr }

func (r request) close() {
	procInternetCloseHandle.Call(r.req)
	procInternetCloseHandle.Call(r.inet)
}

// openURL sends a GET for url with the given extra headers (each
// "Name: value") and WinINet flags, and returns the open request together
// with its HTTP status.
func openURL(url string, headers []string, flags uintptr) (request, uint32, error) {
	agent, err := windows.UTF16PtrFromString(userAgent)
	if err != nil {
		return request{}, 0, err
	}
	inet, _, e := procInternetOpenW.Call(uintptr(unsafe.Pointer(agent)), internetOpenTypePreconfig, 0, 0, 0)
	if inet == 0 {
		return request{}, 0, fmt.Errorf("InternetOpenW: %w", e)
	}

	timeout := httpTimeoutMs
	for _, opt := range []uintptr{internetOptionConnectTimeout, internetOptionReceiveTimeout} {
		procInternetSetOptionW.Call(inet, opt, uintptr(unsafe.Pointer(&timeout)), unsafe.Sizeof(timeout))
	}

	urlPtr, err := windows.UTF16PtrFromString(url)
	if err != nil {
		procInternetCloseHandle.Call(inet)
		return request{}, 0, err
	}
	var headersPtr *uint16
	var headersLen uintptr
	if len(headers) > 0 {
		if headersPtr, err = windows.UTF16PtrFromString(strings.Join(headers, "\r\n")); err != nil {
			procInternetCloseHandle.Call(inet)
			return request{}, 0, err
		}
		headersLen = 0xFFFFFFFF // (DWORD)-1: the headers string is NUL-terminated
	}
	req, _, e := procInternetOpenUrlW.Call(inet,
		uintptr(unsafe.Pointer(urlPtr)), uintptr(unsafe.Pointer(headersPtr)), headersLen,
		internetFlagReload|internetFlagNoCacheWrite|internetFlagNoCookies|internetFlagNoUI|flags, 0)
	if req == 0 {
		procInternetCloseHandle.Call(inet)
		return request{}, 0, fmt.Errorf("InternetOpenUrlW: %w", e)
	}
	r := request{inet, req}

	var status uint32
	size := uint32(unsafe.Sizeof(status))
	if ok, _, e := procHttpQueryInfoW.Call(req, httpQueryStatusCode|httpQueryFlagNumber,
		uintptr(unsafe.Pointer(&status)), uintptr(unsafe.Pointer(&size)), 0); ok == 0 {
		r.close()
		return request{}, 0, fmt.Errorf("HttpQueryInfoW: %w", e)
	}
	return r, status, nil
}

// httpGet fetches url, following redirects, and copies the response body
// to w. Any status other than 200 OK is an error, which includes the start
// of the response body.
func httpGet(url string, headers []string, w io.Writer) error {
	r, status, err := openURL(url, headers, 0)
	if err != nil {
		return err
	}
	defer r.close()

	body := responseReader(r.req)
	if status != 200 {
		return fmt.Errorf("HTTP %d%s", status, errorSnippet(body))
	}
	_, err = io.Copy(w, body)
	return err
}

// redirectTarget requests url without following its redirect and returns
// the address it redirects to.
func redirectTarget(url string) (string, error) {
	r, status, err := openURL(url, nil, internetFlagNoAutoRedirect)
	if err != nil {
		return "", err
	}
	defer r.close()

	if status < 300 || status > 399 {
		return "", fmt.Errorf("HTTP %d from %s where a redirect was expected", status, url)
	}
	buf := make([]uint16, 2048)
	size := uint32(len(buf) * 2) // HttpQueryInfoW counts bytes
	if ok, _, e := procHttpQueryInfoW.Call(r.req, httpQueryLocation,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0); ok == 0 {
		return "", fmt.Errorf("HttpQueryInfoW(Location): %w", e)
	}
	return windows.UTF16ToString(buf), nil
}

// errorSnippet returns the start of an error response's body as one short
// line for an error message ("" if the body is empty). GitHub's 404 pages
// are whole HTML documents.
func errorSnippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 4096))
	s := strings.Join(strings.Fields(string(b)), " ")
	if s == "" {
		return ""
	}
	const limit = 200
	if runes := []rune(s); len(runes) > limit {
		s = string(runes[:limit]) + "..."
	}
	return ": " + s
}

// responseReader reads a WinINet request handle's response body.
type responseReader uintptr

func (h responseReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var n uint32
	if r, _, e := procInternetReadFile.Call(uintptr(h),
		uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)), uintptr(unsafe.Pointer(&n))); r == 0 {
		return 0, fmt.Errorf("InternetReadFile: %w", e)
	}
	if n == 0 {
		return 0, io.EOF
	}
	return int(n), nil
}
