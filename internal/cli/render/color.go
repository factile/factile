package render

func ResolveColor(opts Options) bool {
	if opts.ColorMode == ColorNever {
		return false
	}
	if envValue(opts.Env, "NO_COLOR") != "" || envValue(opts.Env, "TERM") == "dumb" {
		return false
	}
	if opts.ColorMode == ColorAlways {
		return true
	}
	return opts.StdoutIsTerminal
}

func envValue(env map[string]string, key string) string {
	if env != nil {
		return env[key]
	}
	return ""
}
