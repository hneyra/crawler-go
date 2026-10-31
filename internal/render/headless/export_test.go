package headless

// KillForTest stops chrome behind l without clearing it: same state as a crash.
func KillForTest(l *Lazy) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.r != nil {
		l.r.Close()
	}
}
