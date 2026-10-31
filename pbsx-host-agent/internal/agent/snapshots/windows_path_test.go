package snapshots

import "testing"

func TestPlanWindowsSnapshotSource(t *testing.T) {
	tests := []struct {
		source   string
		volume   string
		relative string
	}{
		{source: `C:\`, volume: "C:"},
		{source: `C:\Users\Nyx\Documents`, volume: "C:", relative: `Users\Nyx\Documents`},
		{source: `d:\Projects\PBS-X`, volume: "D:", relative: `Projects\PBS-X`},
	}
	for _, test := range tests {
		got, err := planWindowsSnapshotSource(test.source)
		if err != nil {
			t.Fatalf("planWindowsSnapshotSource(%q): %v", test.source, err)
		}
		if got.Volume != test.volume || got.Relative != test.relative {
			t.Errorf("plan = %#v, want volume=%q relative=%q", got, test.volume, test.relative)
		}
	}
}

func TestPlanWindowsSnapshotSourceRejectsNonWindowsPath(t *testing.T) {
	if _, err := planWindowsSnapshotSource(`/home/user`); err == nil {
		t.Fatal("Linux path was accepted as a Windows VSS source")
	}
}
