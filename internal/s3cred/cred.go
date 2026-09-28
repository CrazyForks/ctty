// Package s3cred stores S3 secret access keys in the shared encrypted credential
// vault (~/.config/ctty/credentials.json, AES-256-GCM, mode 0600).
//
// Keys are namespaced as "s3:<site>" so S3 sites never collide with SSH
// host entries, FTP sites, or WebDAV sites.
package s3cred

import (
	"github.com/zsuroy/ctty/internal/credential"
)

// keyPrefix namespaces S3 entries inside the shared vault.
const keyPrefix = "s3:"

// vaultKey maps a site name to its vault entry name.
func vaultKey(siteName string) string {
	return keyPrefix + siteName
}

// GetSecretKey returns the stored S3 secret access key for a site name.
func GetSecretKey(siteName string) (string, bool) {
	return credential.GetPassword(vaultKey(siteName))
}

// SetSecretKey stores (or updates) the S3 secret access key for a site.
// An empty secret key deletes the entry.
func SetSecretKey(siteName, secretKey string) error {
	return credential.SetPassword(vaultKey(siteName), secretKey)
}

// DeleteSecretKey removes the stored S3 secret access key for a site.
func DeleteSecretKey(siteName string) error {
	return credential.DeletePassword(vaultKey(siteName))
}
