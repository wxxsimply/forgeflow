package buildinfo

import "testing"

func TestNewNormalizesReleaseMetadata(t *testing.T) {
	const commit = "ABCDEF0123456789ABCDEF0123456789ABCDEF01"
	info := New(" 0.12.0-rc.1 ", commit)
	if info.ServiceVersion != "0.12.0-rc.1" || info.GitCommit != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("unexpected build info: %+v", info)
	}
}

func TestNewFailsClosedForUntrustedMetadata(t *testing.T) {
	for _, input := range []string{"", "main", "abc", "abcdef0123456789abcdef0123456789abcdef0g"} {
		info := New("", input)
		if info.ServiceVersion != DevelopmentVersion || info.GitCommit != UnknownCommit {
			t.Fatalf("input=%q info=%+v", input, info)
		}
	}
}
