package product

import (
	"github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivatePathsPreserveContentsAndRejectSymlinks(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	if err := PrivateDirectory(directory); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "config.json")
	if err := os.WriteFile(file, []byte("existing-credential-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PrivateFile(file); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(file)
	if string(content) != "existing-credential-data" {
		t.Fatal("private file must not truncate existing credentials")
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file permissions: %o", info.Mode().Perm())
	}
	info, _ = os.Stat(directory)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("directory permissions: %o", info.Mode().Perm())
	}
	link := filepath.Join(directory, "linked.json")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if err := PrivateFile(link); err == nil {
		t.Fatal("credential symlink accepted")
	}
	if err := PrivateDirectory(link); err == nil {
		t.Fatal("directory symlink accepted")
	}
}

func TestRefreshTokenQueryIsRedacted(t *testing.T) {
	formatter := RedactingFormatter{Inner: &logrus.TextFormatter{DisableTimestamp: true, DisableColors: true}}
	entry := logrus.NewEntry(logrus.New())
	entry.Message = "refresh failed https://example.test/renew?refresh_ui=private-value&sign=signed-value&path=movie"
	content, err := formatter.Format(entry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "private-value") || strings.Contains(string(content), "signed-value") {
		t.Fatal("authorization query leaked")
	}
	if !strings.Contains(string(content), "path=movie") {
		t.Fatal("non-sensitive diagnostics lost")
	}
}

func TestStructuredTokenAndProxyCredentialsAreRedactedBeforeFormatting(t *testing.T) {
	for _, inner := range []logrus.Formatter{&logrus.TextFormatter{}, &logrus.JSONFormatter{}} {
		formatter := RedactingFormatter{Inner: inner}
		entry := logrus.NewEntry(logrus.New())
		entry.Message = `response {"access_token":"access-secret","refresh_token":"refresh-secret","expires_in":7200} from http://proxy-user:proxy-pass@proxy.test:8080`
		entry.Data["error"] = `request https://proxy-user:proxy-pass@proxy.test?token=query-secret&status=expired`
		entry.Data["refresh_token"] = "field-secret"
		original := entry.Message
		content, err := formatter.Format(entry)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"access-secret", "refresh-secret", "proxy-user", "proxy-pass", "query-secret", "field-secret"} {
			if strings.Contains(string(content), secret) {
				t.Fatalf("credential leaked through %T", inner)
			}
		}
		if entry.Message != original || !strings.Contains(string(content), "expires_in") || !strings.Contains(string(content), "status=expired") {
			t.Fatal("formatter mutated shared entry or removed useful diagnostics")
		}
	}
}

func TestCanonicalPathFixesParentAliasAndRejectsLeafLink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	resolved, err := CanonicalPath(filepath.Join(alias, "new", "config.json"))
	expected, _ := filepath.EvalSymlinks(real)
	if err != nil || resolved != filepath.Join(expected, "new", "config.json") {
		t.Fatalf("parent alias not fixed: %s %v", resolved, err)
	}
	if _, err := CanonicalPath(alias); err == nil {
		t.Fatal("leaf symlink accepted as a managed root")
	}
}
