package query

import "testing"

func TestParseGPUs(t *testing.T) {
	data := []byte("1, GPU-2, NVIDIA A100, 48, 75, 256, 40960, 155.5\n0, GPU-1, NVIDIA H100, [N/A], 12, 1, 81920, [N/A]\n")
	all, err := parseGPUs(data, "")
	if err != nil || len(all) != 2 || all[0].Name != "NVIDIA H100" || all[1].Temperature == nil || *all[1].Temperature != 48 || all[1].MemoryUsed == nil || *all[1].MemoryUsed != 256 || all[0].Temperature != nil {
		t.Fatalf("GPU parsing: %+v, %v", all, err)
	}
	filtered, err := parseGPUs(data, "*A100")
	if err != nil || len(filtered) != 1 || filtered[0].Index != 1 || filtered[0].Power == nil || *filtered[0].Power != 155.5 {
		t.Fatalf("GPU filter: %+v, %v", filtered, err)
	}
	for _, bad := range []string{"0, GPU-1, name\n", "x, GPU-1, name, 10, 2, 1, 2, 3\n", "0, GPU-1, name, bogus, 2, 1, 2, 3\n"} {
		if _, err := parseGPUs([]byte(bad), ""); err == nil {
			t.Fatalf("bad GPU row accepted: %q", bad)
		}
	}
}
