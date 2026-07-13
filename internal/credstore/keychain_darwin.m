// Objective-C-Backend für den Touch-ID-Credstore (nur darwin+cgo).
#import <Foundation/Foundation.h>
#import <LocalAuthentication/LocalAuthentication.h>
#import <Security/Security.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

int bwenv_biometry_available(void) {
	LAContext *ctx = [[LAContext alloc] init];
	NSError *err = nil;
	BOOL ok = [ctx canEvaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics error:&err];
	return ok ? 1 : 0;
}

// 1 = Erfolg, 0 = abgelehnt/abgebrochen, -1 = Biometrie nicht verfügbar
int bwenv_biometry_check(const char *reason) {
	LAContext *ctx = [[LAContext alloc] init];
	NSError *err = nil;
	if (![ctx canEvaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics error:&err]) {
		return -1;
	}
	dispatch_semaphore_t sem = dispatch_semaphore_create(0);
	__block int result = 0;
	NSString *r = [NSString stringWithUTF8String:reason];
	[ctx evaluatePolicy:LAPolicyDeviceOwnerAuthenticationWithBiometrics
	    localizedReason:r
	              reply:^(BOOL success, NSError *error) {
		            result = success ? 1 : 0;
		            dispatch_semaphore_signal(sem);
	              }];
	dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);
	return result;
}

static NSDictionary *bwenv_base_query(const char *service, const char *account) {
	return @{
		(__bridge id)kSecClass : (__bridge id)kSecClassGenericPassword,
		(__bridge id)kSecAttrService : [NSString stringWithUTF8String:service],
		(__bridge id)kSecAttrAccount : [NSString stringWithUTF8String:account],
	};
}

// 0 = ok, sonst OSStatus
int bwenv_keychain_set(const char *service, const char *account, const uint8_t *data, size_t len) {
	NSDictionary *query = bwenv_base_query(service, account);
	SecItemDelete((__bridge CFDictionaryRef)query);
	NSMutableDictionary *add = [query mutableCopy];
	add[(__bridge id)kSecValueData] = [NSData dataWithBytes:data length:len];
	OSStatus st = SecItemAdd((__bridge CFDictionaryRef)add, NULL);
	return (int)st;
}

// 1 = vorhanden, 0 = nicht vorhanden, sonst negativer OSStatus
int bwenv_keychain_exists(const char *service, const char *account) {
	NSMutableDictionary *query = [bwenv_base_query(service, account) mutableCopy];
	query[(__bridge id)kSecMatchLimit] = (__bridge id)kSecMatchLimitOne;
	OSStatus st = SecItemCopyMatching((__bridge CFDictionaryRef)query, NULL);
	if (st == errSecSuccess) {
		return 1;
	}
	if (st == errSecItemNotFound) {
		return 0;
	}
	return (int)st < 0 ? (int)st : -(int)st;
}

// 0 = ok (out muss mit free() freigegeben werden), sonst OSStatus
int bwenv_keychain_get(const char *service, const char *account, uint8_t **out, size_t *outlen) {
	NSMutableDictionary *query = [bwenv_base_query(service, account) mutableCopy];
	query[(__bridge id)kSecReturnData] = @YES;
	query[(__bridge id)kSecMatchLimit] = (__bridge id)kSecMatchLimitOne;
	CFTypeRef result = NULL;
	OSStatus st = SecItemCopyMatching((__bridge CFDictionaryRef)query, &result);
	if (st != errSecSuccess) {
		return (int)st;
	}
	NSData *d = (__bridge_transfer NSData *)result;
	*out = malloc(d.length);
	memcpy(*out, d.bytes, d.length);
	*outlen = d.length;
	return 0;
}

// 0 = ok (auch wenn nichts da war), sonst OSStatus
int bwenv_keychain_delete(const char *service, const char *account) {
	OSStatus st = SecItemDelete((__bridge CFDictionaryRef)bwenv_base_query(service, account));
	if (st == errSecItemNotFound) {
		return 0;
	}
	return (int)st;
}
