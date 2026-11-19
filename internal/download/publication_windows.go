package download

import (
	"github.com/Dev-Wonjoon/StorageBucket/internal/media"
	"os"
)

func openPublicationFile(root *os.Root, path string) (*os.File, error) {
	return media.OpenOwnedFile(root, path)
}
func publicationIdentity(f *os.File) (string, error) { return media.FileIdentity(f) }
func verifyPublicationHandle(f *os.File, e publicationEntry) error {
	return media.VerifyOwnedFile(f, media.FileProof{Path: e.Path, Identity: e.Identity, Hash: e.Hash, Size: e.Size})
}
func renamePublicationFile(root *os.Root, f *os.File, path string) error {
	return media.RenameOwnedFile(root, f, path)
}
func deletePublicationFile(f *os.File) error { return media.DeleteOwnedFile(f) }
