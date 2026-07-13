//go:build darwin && cgo

package credstore

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework LocalAuthentication -framework Security
#include <stdint.h>
#include <stdlib.h>
int bwenv_biometry_available(void);
int bwenv_biometry_check(const char *reason);
int bwenv_keychain_set(const char *service, const char *account, const uint8_t *data, size_t len);
int bwenv_keychain_exists(const char *service, const char *account);
int bwenv_keychain_get(const char *service, const char *account, uint8_t **out, size_t *outlen);
int bwenv_keychain_delete(const char *service, const char *account);
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

const (
	kcService = "com.leusmann.bwenv"
	kcAccount = "master-password"
)

// darwinStore nutzt LocalAuthentication (Touch-ID-Gate) + Login-Keychain.
type darwinStore struct{}

// New liefert den plattformspezifischen Store.
func New() Store { return darwinStore{} }

func (darwinStore) Available() bool {
	return C.bwenv_biometry_available() == 1
}

func (darwinStore) Enrolled() bool {
	service, account := cStrings()
	defer freeStrings(service, account)
	return C.bwenv_keychain_exists(service, account) == 1
}

func (darwinStore) Enroll(secret string) error {
	if rc := biometryCheck("Touch-ID-Unlock für bwenv einrichten"); rc != nil {
		return rc
	}
	service, account := cStrings()
	defer freeStrings(service, account)
	data := []byte(secret)
	st := C.bwenv_keychain_set(service, account, (*C.uint8_t)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
	if st != 0 {
		return fmt.Errorf("keychain-Fehler (OSStatus %d)", int(st))
	}
	return nil
}

func (darwinStore) Retrieve(reason string) (string, error) {
	service, account := cStrings()
	defer freeStrings(service, account)
	if C.bwenv_keychain_exists(service, account) != 1 {
		return "", ErrNotEnrolled
	}
	if rc := biometryCheck(reason); rc != nil {
		return "", rc
	}
	var out *C.uint8_t
	var outlen C.size_t
	st := C.bwenv_keychain_get(service, account, &out, &outlen)
	if st != 0 {
		return "", fmt.Errorf("keychain-Fehler (OSStatus %d)", int(st))
	}
	defer C.free(unsafe.Pointer(out))
	return C.GoStringN((*C.char)(unsafe.Pointer(out)), C.int(outlen)), nil
}

func (darwinStore) Erase() error {
	service, account := cStrings()
	defer freeStrings(service, account)
	if st := C.bwenv_keychain_delete(service, account); st != 0 {
		return fmt.Errorf("keychain-Fehler (OSStatus %d)", int(st))
	}
	return nil
}

func biometryCheck(reason string) error {
	r := C.CString(reason)
	defer C.free(unsafe.Pointer(r))
	switch C.bwenv_biometry_check(r) {
	case 1:
		return nil
	case -1:
		return ErrUnavailable
	default:
		return errors.New("Touch-ID-Prüfung abgebrochen oder fehlgeschlagen")
	}
}

func cStrings() (service, account *C.char) {
	return C.CString(kcService), C.CString(kcAccount)
}

func freeStrings(service, account *C.char) {
	C.free(unsafe.Pointer(service))
	C.free(unsafe.Pointer(account))
}
