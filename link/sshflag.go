package link

import "strings"

// forceSSHTTY adds -tt to an "ssh ..." command line that doesn't already ask
// for a terminal. On Windows a local program runs without a pseudo-terminal,
// so ssh would otherwise refuse to allocate one on the remote host
// ("Pseudo-terminal will not be allocated because stdin is not a terminal").
func forceSSHTTY(command string) string {
	trimmed := strings.TrimSpace(command)
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return command
	}
	prog := strings.ToLower(fields[0])
	prog = strings.TrimSuffix(prog, ".exe")
	if i := strings.LastIndexAny(prog, `/\`); i >= 0 {
		prog = prog[i+1:]
	}
	if prog != "ssh" {
		return command
	}
	for _, f := range fields[1:] {
		if f == "-t" || f == "-tt" || f == "-T" {
			return command
		}
	}
	return fields[0] + " -tt" + trimmed[len(fields[0]):]
}
