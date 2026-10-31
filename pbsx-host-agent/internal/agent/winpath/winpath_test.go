package winpath

import "testing"

func TestParseAbsoluteAndAlias(t *testing.T) {
	tests := []struct {
		input    string
		volume   string
		relative string
		alias    string
	}{
		{input: `c:\`, volume: "C:", alias: "C"},
		{input: `C:\Users\Nyx\Documents`, volume: "C:", relative: `Users\Nyx\Documents`, alias: "C-Users-Nyx-Documents"},
		{input: `\\server\share\Backups\Host`, volume: `\\server\share`, relative: `Backups\Host`, alias: "server-share-Backups-Host"},
	}
	for _, test := range tests {
		parsed, err := ParseAbsolute(test.input)
		if err != nil {
			t.Fatalf("ParseAbsolute(%q): %v", test.input, err)
		}
		if parsed.Volume != test.volume || parsed.RelativeWindows() != test.relative || parsed.Alias() != test.alias {
			t.Errorf("ParseAbsolute(%q) = %#v rel=%q alias=%q", test.input, parsed, parsed.RelativeWindows(), parsed.Alias())
		}
	}
}

func TestRelativeToUsesWindowsCaseFolding(t *testing.T) {
	base, _ := ParseAbsolute(`C:\Users\Nyx`)
	target, _ := ParseAbsolute(`c:\users\NYX\Documents\File.txt`)
	relative, inside := RelativeTo(base, target)
	if !inside || relative != "Documents/File.txt" {
		t.Fatalf("RelativeTo = %q, %t", relative, inside)
	}

	other, _ := ParseAbsolute(`D:\Users\Nyx\File.txt`)
	if _, inside := RelativeTo(base, other); inside {
		t.Fatal("path on another drive was treated as inside source")
	}
}

func TestCleanRelativeRejectsTraversal(t *testing.T) {
	if got, err := CleanRelative(`Cache\tmp\file`); err != nil || got != "Cache/tmp/file" {
		t.Fatalf("CleanRelative = %q, %v", got, err)
	}
	if _, err := CleanRelative(`..\Windows`); err == nil {
		t.Fatal("parent traversal was accepted")
	}
}
