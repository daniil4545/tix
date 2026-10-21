package gitx

import "testing"

func TestScrub_RemovesUserinfo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"fatal: unable to access 'https://user:secret@host/r.git/'", "fatal: unable to access 'https://host/r.git/'"},
		{"https://user:p@ss@host/repo", "https://host/repo"},
		{"http://user:S3cr/etPw@127.0.0.1:18999/r.git", "http://127.0.0.1:18999/r.git"},
		{"https://host:8443/team/r.git", "https://host:8443/team/r.git"},
		{"https://token@host/repo", "https://host/repo"},
		{"remote: no url here", "remote: no url here"},
	}
	for _, c := range cases {
		if got := scrub(c.in); got != c.want {
			t.Errorf("scrub(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
