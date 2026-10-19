package link

import "testing"

func TestForceSSHTTY(t *testing.T) {
	cases := map[string]string{
		"ssh bam@100.1.2.3":                      "ssh -tt bam@100.1.2.3",
		"  ssh -p 2222 bam@pi":                   "ssh -tt -p 2222 bam@pi",
		"ssh.exe bam@pi":                         "ssh.exe -tt bam@pi",
		`C:\Windows\System32\OpenSSH\ssh.exe pi`: `C:\Windows\System32\OpenSSH\ssh.exe -tt pi`,
		"ssh -t bam@pi":                          "ssh -t bam@pi",
		"ssh -tt bam@pi":                         "ssh -tt bam@pi",
		"simh nova.ini":                          "simh nova.ini",
		"":                                       "",
	}
	for in, want := range cases {
		if got := forceSSHTTY(in); got != want {
			t.Errorf("forceSSHTTY(%q) = %q, want %q", in, got, want)
		}
	}
}
