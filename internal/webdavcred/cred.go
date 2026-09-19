// Package webdavcred stores WebDAV passwords in the shared encrypted credential
// vault (~/.config/ctty/credentials.json, AES-256-GCM, mode 0600).
//
// Keys are namespaced as "webdav:<site>" so WebDAV sites never collide with SSH
// host entries or FTP sites and never surface in SSH_ASKPASS lookups.
package webdavcred

import (
	"github.com/zsuroy/ctty/internal/credential"
)

// keyPrefix namespaces WebDAV entries inside the shared vault.
const keyPrefix = "webdav:"

// vaultKey maps a site name to its vault entry name.
func vaultKey(siteName string) string {
	return keyPrefix + siteName
}

// GetPassword returns the stored WebDAV password for a site name.
func GetPassword(siteName string) (string, bool) {
	return credential.GetPassword(vaultKey(siteName))
}

// SetPassword stores (or updates) the WebDAV password for a site.
// An empty password deletes the entry.
func SetPassword(siteName, password string) error {
	return credential.SetPassword(vaultKey(siteName), password)
}

// DeletePassword removes the stored WebDAV password for a site.
func DeletePassword(siteName string) error {
	return credential.DeletePassword(vaultKey(siteName))
}
