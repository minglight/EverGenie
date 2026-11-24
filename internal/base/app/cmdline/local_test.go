package cmdline

import "testing"

// 本機指令的旗標：--name 值、--name=值、開關；位置參數留在 rest；認不得的旗標或缺值回 ok=false。
func TestLocalFlags(t *testing.T) {
	vals, on, rest, ok := LocalFlags([]string{"--home", "/h", "--to=v1", "--apply", "pos"}, []string{"home", "to"}, []string{"apply"})
	if !ok || vals["home"] != "/h" || vals["to"] != "v1" || !on["apply"] || len(rest) != 1 || rest[0] != "pos" {
		t.Fatalf("%v %v %v %v", vals, on, rest, ok)
	}
	for _, bad := range [][]string{{"--unknown"}, {"--home"}, {"--apply=1"}} {
		if _, _, _, ok := LocalFlags(bad, []string{"home"}, []string{"apply"}); ok {
			t.Errorf("%v 應該不合法", bad)
		}
	}
}
