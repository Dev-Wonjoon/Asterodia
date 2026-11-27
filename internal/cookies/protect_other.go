//go:build !windows

package cookies

import "fmt"

func protect([]byte) ([]byte, error) {
	return nil, fmt.Errorf("쿠키 저장소의 운영체제 암호화를 사용할 수 없습니다")
}
func unprotect([]byte) ([]byte, error) {
	return nil, fmt.Errorf("쿠키 저장소의 운영체제 암호화를 사용할 수 없습니다")
}
