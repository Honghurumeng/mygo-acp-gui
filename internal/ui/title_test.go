package ui

import "testing"

func TestCleanSessionTitleStripsAttachmentPaths(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{
			raw:  "/var/folders/8v/xyz/T/pi-clipboard-a6dca4df.png 右上角这几个按钮是做什么的",
			want: "右上角这几个按钮是做什么的",
		},
		{
			raw:  "/var/folders/a/one.png /private/var/folders/b/two.jpg 把背景改成蓝色天空",
			want: "把背景改成蓝色天空",
		},
		{
			raw:  "/var/folders/8v/xyz/T/pi-clipboard-a6dca4df",
			want: imageSessionTitle,
		},
		{raw: "  修复会话名称  ", want: "修复会话名称"},
		{raw: "https://example.com/docs 看看", want: "https://example.com/docs 看看"},
		{raw: "/name My Session", want: "/name My Session"},
		{raw: "", want: ""},
	}
	for _, tc := range cases {
		if got := cleanSessionTitle(tc.raw); got != tc.want {
			t.Errorf("cleanSessionTitle(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestStripLeadingAttachmentPathsReportsPaths(t *testing.T) {
	text, had := stripLeadingAttachmentPaths("hello world")
	if text != "hello world" || had {
		t.Errorf("plain text = %q, %v", text, had)
	}
	text, had = stripLeadingAttachmentPaths("/var/folders/a.png hello")
	if text != "hello" || !had {
		t.Errorf("path + text = %q, %v", text, had)
	}
}
