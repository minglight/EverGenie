package violation

// uses tmux
func bad(engine string) bool {
	if engine == "claude" {
		return true
	}
	return false
}
