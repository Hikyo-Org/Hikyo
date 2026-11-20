//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <stdlib.h>
#include <string.h>
#include <Security/Security.h>

#define HIKYO_SERVICE "io.hikyo.desktop"

static CFMutableDictionaryRef hkQuery(const char *account) {
	CFMutableDictionaryRef q = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFStringRef service = CFStringCreateWithCString(NULL, HIKYO_SERVICE, kCFStringEncodingUTF8);
	CFStringRef acct = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
	CFDictionarySetValue(q, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(q, kSecAttrService, service);
	CFDictionarySetValue(q, kSecAttrAccount, acct);
	CFRelease(service);
	CFRelease(acct);
	return q;
}

// hkPut stores or replaces the item. The legacy file keychain is used on
// purpose: its default access control trusts only the application that
// created the item, which is the signed app (ADR D3). The data protection
// keychain would need entitlements and a provisioning profile.
static int hkPut(const char *account, const void *data, int len) {
	CFMutableDictionaryRef q = hkQuery(account);
	CFDataRef value = CFDataCreate(NULL, data, len);
	CFMutableDictionaryRef update = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(update, kSecValueData, value);
	OSStatus status = SecItemUpdate(q, update);
	if (status == errSecItemNotFound) {
		CFDictionarySetValue(q, kSecValueData, value);
		status = SecItemAdd(q, NULL);
	}
	CFRelease(update);
	CFRelease(value);
	CFRelease(q);
	return (int)status;
}

// hkGet returns a malloc'd copy of the item's data and its length.
static int hkGet(const char *account, void **out, int *len) {
	CFMutableDictionaryRef q = hkQuery(account);
	CFDictionarySetValue(q, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(q, kSecMatchLimit, kSecMatchLimitOne);
	CFTypeRef result = NULL;
	OSStatus status = SecItemCopyMatching(q, &result);
	CFRelease(q);
	if (status != errSecSuccess) {
		return (int)status;
	}
	CFDataRef data = (CFDataRef)result;
	*len = (int)CFDataGetLength(data);
	*out = malloc(*len > 0 ? *len : 1);
	memcpy(*out, CFDataGetBytePtr(data), *len);
	CFRelease(result);
	return 0;
}

static int hkDelete(const char *account) {
	CFMutableDictionaryRef q = hkQuery(account);
	OSStatus status = SecItemDelete(q);
	CFRelease(q);
	return (int)status;
}

static char *hkMessage(int status) {
	CFStringRef message = SecCopyErrorMessageString((OSStatus)status, NULL);
	if (message == NULL) {
		return NULL;
	}
	char buf[256];
	char *out = NULL;
	if (CFStringGetCString(message, buf, sizeof buf, kCFStringEncodingUTF8)) {
		out = strdup(buf);
	}
	CFRelease(message);
	return out;
}
*/
import "C"

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"unsafe"

	"github.com/Hikyo-Org/hikyo/desktop/internal/shell"
)

const errSecItemNotFound = -25300

// keychain stores one generic-password item per connection, holding the token
// and its session id together (ADR D3).
type keychain struct{}

func statusError(op string, status C.int) error {
	text := ""
	if msg := C.hkMessage(status); msg != nil {
		text = C.GoString(msg)
		C.free(unsafe.Pointer(msg))
	}
	return fmt.Errorf("keychain %s failed (OSStatus %d) %s", op, int(status), text)
}

func (keychain) Get(host string) (shell.Secret, error) {
	account := C.CString(host)
	defer C.free(unsafe.Pointer(account))
	var data unsafe.Pointer
	var n C.int
	if status := C.hkGet(account, &data, &n); status != 0 {
		if int(status) == errSecItemNotFound {
			return shell.Secret{}, shell.ErrNoSecret
		}
		return shell.Secret{}, statusError("read", status)
	}
	defer C.free(data)
	var secret shell.Secret
	if err := json.Unmarshal(C.GoBytes(data, n), &secret, json.RejectUnknownMembers(true)); err != nil {
		return shell.Secret{}, errors.New("the stored session is unreadable")
	}
	if secret.Token == "" || secret.SessionID == "" {
		return shell.Secret{}, errors.New("the stored session is incomplete")
	}
	return secret, nil
}

func (keychain) Put(host string, s shell.Secret) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	account := C.CString(host)
	defer C.free(unsafe.Pointer(account))
	if status := C.hkPut(account, unsafe.Pointer(&raw[0]), C.int(len(raw))); status != 0 {
		return statusError("write", status)
	}
	return nil
}

func (keychain) Delete(host string) error {
	account := C.CString(host)
	defer C.free(unsafe.Pointer(account))
	if status := C.hkDelete(account); status != 0 && int(status) != errSecItemNotFound {
		return statusError("delete", status)
	}
	return nil
}
