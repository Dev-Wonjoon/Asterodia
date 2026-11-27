package cookies

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

func crypt(body []byte, decrypt bool) ([]byte, error) {
	input := windows.DataBlob{Size: uint32(len(body))}
	if len(body) > 0 {
		input.Data = &body[0]
	}
	var output windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	} else {
		err = windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, int(output.Size))...), nil
}
func protect(body []byte) ([]byte, error)   { return crypt(body, false) }
func unprotect(body []byte) ([]byte, error) { return crypt(body, true) }
