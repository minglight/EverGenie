package platform

import "testing"

// 主人是 owner(<入口>)；秘書、system、空字串、長得像但不是前綴的都不是。
func TestIsOwner(t *testing.T) {
	for by, want := range map[string]bool{
		"owner(web)": true, "owner(cli)": true, "owner(": true,
		"": false, "system": false, "life": false, "owner": false, "xowner(web)": false, "Owner(web)": false,
	} {
		if got := IsOwner(by); got != want {
			t.Errorf("IsOwner(%q) = %v, want %v", by, got, want)
		}
	}
}

// 主人或系統：system 也算；秘書、空字串不算。
func TestIsOwnerOrSystem(t *testing.T) {
	for by, want := range map[string]bool{
		"owner(web)": true, "system": true,
		"": false, "life": false, "systems": false, "xowner(web)": false,
	} {
		if got := IsOwnerOrSystem(by); got != want {
			t.Errorf("IsOwnerOrSystem(%q) = %v, want %v", by, got, want)
		}
	}
}
