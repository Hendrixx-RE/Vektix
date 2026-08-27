package fileops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hendrixx-RE/Vektix/internal/config"
)

func TestResolvePath_Confinement(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create some directories and files
	root1 := filepath.Join(tmpDir, "root1")
	root2 := filepath.Join(tmpDir, "root2")
	outside := filepath.Join(tmpDir, "outside")
	
	os.MkdirAll(root1, 0755)
	os.MkdirAll(root2, 0755)
	os.MkdirAll(outside, 0755)
	
	file1 := filepath.Join(root1, "file1.txt")
	os.WriteFile(file1, []byte("test"), 0644)
	
	fileOutside := filepath.Join(outside, "fileOutside.txt")
	os.WriteFile(fileOutside, []byte("test"), 0644)
	
	symlinkToOutside := filepath.Join(root1, "symlinkOutside")
	os.Symlink(fileOutside, symlinkToOutside)
	
	cfg := config.DefaultConfig()
	cfg.Safety.ConfineToRoots = true
	cfg.Safety.AllowSecrets = false
	cfg.Index.IndexDirs = []string{root1, root2}
	
	// Test inside root
	if _, err := ResolvePath(file1, false, &cfg); err != nil {
		t.Errorf("Expected file1 to be allowed, got err: %v", err)
	}
	
	// Test traversal
	traversalPath := filepath.Join(root1, "..", "outside", "fileOutside.txt")
	if _, err := ResolvePath(traversalPath, false, &cfg); err == nil {
		t.Errorf("Expected traversal outside root to be denied")
	}
	
	// Test absolute path outside roots
	if _, err := ResolvePath(fileOutside, false, &cfg); err == nil {
		t.Errorf("Expected absolute path outside root to be denied")
	}
	
	// Test symlink escaping root
	if _, err := ResolvePath(symlinkToOutside, false, &cfg); err == nil {
		t.Errorf("Expected symlink escaping root to be denied")
	}
	
	// Test explicit unsafe bypasses confinement
	if _, err := ResolvePath(fileOutside, true, &cfg); err != nil {
		t.Errorf("Expected explicitUnsafe to bypass confinement, got err: %v", err)
	}
}

func TestResolvePath_Secrets(t *testing.T) {
	tmpDir := t.TempDir()
	root := filepath.Join(tmpDir, "root")
	os.MkdirAll(root, 0755)
	
	cfg := config.DefaultConfig()
	cfg.Safety.ConfineToRoots = true
	cfg.Safety.AllowSecrets = false
	cfg.Index.IndexDirs = []string{root}
	
	secrets := []string{
		filepath.Join(root, ".ssh", "id_rsa"),
		filepath.Join(root, ".gnupg", "pubring.kbx"),
		filepath.Join(root, ".aws", "credentials"),
		filepath.Join(root, "project", ".aws", "credentials"),
		filepath.Join(root, "key.pem"),
		filepath.Join(root, "cert.key"),
		filepath.Join(root, ".env.local"),
		filepath.Join(root, ".env"),
	}
	
	for _, secret := range secrets {
		dir := filepath.Dir(secret)
		os.MkdirAll(dir, 0755)
		os.WriteFile(secret, []byte("secret"), 0600)
		
		if _, err := ResolvePath(secret, false, &cfg); err == nil {
			t.Errorf("Expected secret %s to be denied", secret)
		}
		
		if _, err := ResolvePath(secret, true, &cfg); err != nil {
			t.Errorf("Expected explicitUnsafe to bypass secret check for %s", secret)
		}
	}
	
	// Test innocent files
	innocents := []string{
		filepath.Join(root, "ssh_config"), // not .ssh/
		filepath.Join(root, "my_env.txt"), // not .env*
		filepath.Join(root, "pem_key.txt"), // not *.pem
	}
	
	for _, innocent := range innocents {
		os.WriteFile(innocent, []byte("test"), 0644)
		if _, err := ResolvePath(innocent, false, &cfg); err != nil {
			t.Errorf("Expected innocent file %s to be allowed, got err: %v", innocent, err)
		}
	}
}
