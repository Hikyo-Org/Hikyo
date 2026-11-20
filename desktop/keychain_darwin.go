//go:build darwin

package main

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <stdlib.h>
#include <stdio.h>
#include <pwd.h>
#include <unistd.h>
#include <string.h>
#include <Security/Security.h>

#define HIKYO_SERVICE "io.hikyo.desktop"

static SecKeychainRef hkLogin(void) {
	// The passwd entry, not $HOME: the path must not depend on the environment.
	struct passwd *pw = getpwuid(getuid());
	if (pw == NULL || pw->pw_dir == NULL) {
		return NULL;
	}
	char path[1024];
	if (snprintf(path, sizeof path, "%s/Library/Keychains/login.keychain-db", pw->pw_dir) >= (int)sizeof path) {
		return NULL;
	}
	SecKeychainRef kc = NULL;
	if (SecKeychainOpen(path, &kc) != errSecSuccess) {
		return NULL;
	}
	return kc;
}

#define HK_NO_KEYCHAIN (-25294) // errSecNoSuchKeychain

static CFMutableDictionaryRef hkQuery(const char *account) {
	CFMutableDictionaryRef q = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFStringRef service = CFStringCreateWithCString(NULL, HIKYO_SERVICE, kCFStringEncodingUTF8);
	CFStringRef acct = CFStringCreateWithCString(NULL, account, kCFStringEncodingUTF8);
	CFDictionarySetValue(q, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(q, kSecAttrService, service);
	CFDictionarySetValue(q, kSecAttrAccount, acct);
	CFRelease(service);
	CFRelease(acct);
	// Only the login keychain, opened by its path: neither the search list
	// nor the default keychain, both of which another process of the same user
	// can change. If it can't be opened the query fails closed.
	SecKeychainRef login = hkLogin();
	if (login == NULL) {
		CFRelease(q);
		return NULL;
	}
	CFArrayRef list = CFArrayCreate(NULL, (const void **)&login, 1, &kCFTypeArrayCallBacks);
	CFDictionarySetValue(q, kSecMatchSearchList, list);
	CFDictionarySetValue(q, kSecUseKeychain, login);
	CFRelease(list);
	CFRelease(login);
	return q;
}

// hkRemove deletes the item by reference. SecItemDelete on the query fails
// with errSecInvalidOwnerEdit for an item that another build (or location) of
// this app created, which would leave a refused connection impossible to sign
// in again or remove; deleting the found reference works for the user's own
// item and never reads its data.
static int hkRemove(CFMutableDictionaryRef q) {
	CFDictionarySetValue(q, kSecReturnRef, kCFBooleanTrue);
	CFDictionarySetValue(q, kSecMatchLimit, kSecMatchLimitOne);
	CFTypeRef item = NULL;
	OSStatus status = SecItemCopyMatching(q, &item);
	if (status != errSecSuccess) {
		return (int)status;
	}
	status = SecKeychainItemDelete((SecKeychainItemRef)item);
	CFRelease(item);
	return (int)status;
}

// hkPut replaces the item. It always deletes first and then adds with an
// explicit access list naming only this application. SecItemUpdate would keep
// the access list of whatever item already exists, so an item another process
// pre-planted under our name, with itself in the list, would keep reading
// every token we later stored. The legacy file keychain is used on purpose:
// the data protection keychain would need entitlements and a provisioning
// profile (ADR D3). If the add fails, no item remains: a signed-out
// connection is the safe failure.
static int hkPut(const char *account, const void *data, int len) {
	CFMutableDictionaryRef q = hkQuery(account);
	if (q == NULL) {
		return HK_NO_KEYCHAIN;
	}
	OSStatus status = hkRemove(q);
	if (status != errSecSuccess && status != errSecItemNotFound) {
		CFRelease(q);
		return (int)status;
	}
	CFDictionaryRemoveValue(q, kSecReturnRef);
	CFDictionaryRemoveValue(q, kSecMatchLimit);
	SecTrustedApplicationRef self = NULL;
	status = SecTrustedApplicationCreateFromPath(NULL, &self);
	if (status != errSecSuccess) {
		CFRelease(q);
		return (int)status;
	}
	CFArrayRef trusted = CFArrayCreate(NULL, (const void **)&self, 1, &kCFTypeArrayCallBacks);
	SecAccessRef access = NULL;
	CFStringRef label = CFStringCreateWithCString(NULL, "Hikyo Desktop session", kCFStringEncodingUTF8);
	status = SecAccessCreate(label, trusted, &access);
	CFRelease(label);
	CFRelease(trusted);
	CFRelease(self);
	if (status != errSecSuccess) {
		CFRelease(q);
		return (int)status;
	}
	CFDataRef value = CFDataCreate(NULL, data, len);
	CFDictionarySetValue(q, kSecValueData, value);
	CFDictionarySetValue(q, kSecAttrAccess, access);
	status = SecItemAdd(q, NULL);
	CFRelease(value);
	CFRelease(access);
	CFRelease(q);
	return (int)status;
}

#define HK_UNTRUSTED (-1) // not an OSStatus: the item's access list is not exactly this app

// Exported by Security.framework, declared in its private SecTrustedApplicationPriv.h.
// SecTrustedApplicationCopyData returns only the path the entry was created
// with, which changes when the app is moved or translocated; the requirement
// is what securityd enforces on decrypt.
// Weak, so a macOS that stops exporting it refuses every item instead of
// failing to launch.
extern OSStatus SecTrustedApplicationCopyRequirement(SecTrustedApplicationRef app, SecRequirementRef *requirement) __attribute__((weak_import));

// hkRequirementString renders a requirement as its canonical text.
static CFStringRef hkRequirementString(SecRequirementRef req) {
	CFStringRef text = NULL;
	if (SecRequirementCopyString(req, kSecCSDefaultFlags, &text) != errSecSuccess) {
		return NULL;
	}
	return text;
}

// hkSelfRequirement is this running application's designated requirement, or
// NULL if it has none (an unsigned binary): then nothing can match.
static CFStringRef hkSelfRequirement(void) {
	SecCodeRef code = NULL;
	SecRequirementRef req = NULL;
	CFStringRef text = NULL;
	if (SecCodeCopySelf(kSecCSDefaultFlags, &code) == errSecSuccess) {
		if (SecCodeCopyDesignatedRequirement(code, kSecCSDefaultFlags, &req) == errSecSuccess) {
			text = hkRequirementString(req);
			CFRelease(req);
		}
		CFRelease(code);
	}
	return text;
}

// hkOnlyUs reports whether this application, and nothing else, may decrypt the
// item. The entry is matched by code requirement, not path, so a signed app
// that was moved still matches while another signer does not. An ad-hoc build's
// requirement is its own cdhash, so only the identical binary matches. A NULL
// application list means any application can decrypt, which is what an item
// planted with `security add-generic-password -A` has. Every failure to
// establish the list counts as "not only us".
static int hkOnlyUs(SecKeychainItemRef item) {
	if (SecTrustedApplicationCopyRequirement == NULL) {
		return 0;
	}
	CFStringRef selfReq = hkSelfRequirement();
	if (selfReq == NULL) {
		return 0;
	}
	SecAccessRef access = NULL;
	if (SecKeychainItemCopyAccess(item, &access) != errSecSuccess || access == NULL) {
		CFRelease(selfReq);
		return 0;
	}
	CFArrayRef acls = SecAccessCopyMatchingACLList(access, kSecACLAuthorizationDecrypt);
	int ok = acls != NULL && CFArrayGetCount(acls) > 0;
	for (CFIndex i = 0; ok && i < CFArrayGetCount(acls); i++) {
		CFArrayRef apps = NULL;
		CFStringRef desc = NULL;
		SecKeychainPromptSelector selector;
		if (SecACLCopyContents((SecACLRef)CFArrayGetValueAtIndex(acls, i), &apps, &desc, &selector) != errSecSuccess || apps == NULL || CFArrayGetCount(apps) != 1) {
			ok = 0;
		} else {
			SecRequirementRef req = NULL;
			CFStringRef text = NULL;
			if (SecTrustedApplicationCopyRequirement((SecTrustedApplicationRef)CFArrayGetValueAtIndex(apps, 0), &req) != errSecSuccess || req == NULL) {
				ok = 0;
			} else if ((text = hkRequirementString(req)) == NULL || !CFEqual(text, selfReq)) {
				ok = 0;
			}
			if (text != NULL) {
				CFRelease(text);
			}
			if (req != NULL) {
				CFRelease(req);
			}
		}
		if (apps != NULL) {
			CFRelease(apps);
		}
		if (desc != NULL) {
			CFRelease(desc);
		}
	}
	if (acls != NULL) {
		CFRelease(acls);
	}
	CFRelease(access);
	CFRelease(selfReq);
	return ok;
}

// hkGet returns a malloc'd copy of the item's data and its length. It finds the
// item by reference first, checks its access list, and reads the data from that
// same reference, so an item swapped in between is not the one read.
static int hkGet(const char *account, void **out, int *len) {
	CFMutableDictionaryRef q = hkQuery(account);
	if (q == NULL) {
		return HK_NO_KEYCHAIN;
	}
	CFDictionarySetValue(q, kSecReturnRef, kCFBooleanTrue);
	CFDictionarySetValue(q, kSecMatchLimit, kSecMatchLimitOne);
	CFTypeRef item = NULL;
	OSStatus status = SecItemCopyMatching(q, &item);
	CFRelease(q);
	if (status != errSecSuccess) {
		return (int)status;
	}
	if (!hkOnlyUs((SecKeychainItemRef)item)) {
		CFRelease(item);
		return HK_UNTRUSTED;
	}
	CFMutableDictionaryRef rq = CFDictionaryCreateMutable(NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFDictionarySetValue(rq, kSecValueRef, item);
	CFDictionarySetValue(rq, kSecReturnData, kCFBooleanTrue);
	CFTypeRef result = NULL;
	status = SecItemCopyMatching(rq, &result);
	CFRelease(rq);
	CFRelease(item);
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
	if (q == NULL) {
		return HK_NO_KEYCHAIN;
	}
	OSStatus status = hkRemove(q);
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

// keychain stores one generic-password item per connection, holding the token,
// its session id and the origin, trust and account they are bound to (ADR D3).
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
		if int(status) == C.HK_UNTRUSTED {
			return shell.Secret{}, shell.ErrItemUntrusted
		}
		return shell.Secret{}, statusError("read", status)
	}
	defer C.free(data)
	var secret shell.Secret
	if err := json.Unmarshal(C.GoBytes(data, n), &secret, json.RejectUnknownMembers(true)); err != nil {
		return shell.Secret{}, errors.New("the stored session is unreadable")
	}
	if secret.Token == "" || secret.SessionID == "" || secret.Origin == "" || secret.TrustMode == "" {
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
