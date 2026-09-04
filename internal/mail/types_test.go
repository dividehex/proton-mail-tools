package mail

import "testing"

func TestKindOf(t *testing.T) {
	for name, want := range map[string]string{"INBOX": KindSystem, "Folders/Work": KindFolder, "Labels/Receipts": KindLabel, "Trash": KindSystem} {
		if got := KindOf(name); got != want {
			t.Errorf("KindOf(%q) = %q want %q", name, got, want)
		}
	}
}

func TestKindPrefix(t *testing.T) {
	for kind, want := range map[string]string{KindFolder: FolderPrefix, KindLabel: LabelPrefix, KindSystem: "", "": ""} {
		if got := KindPrefix(kind); got != want {
			t.Errorf("KindPrefix(%q) = %q want %q", kind, got, want)
		}
	}
}
