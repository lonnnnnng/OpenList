package quark

import (
	"net/url"
	"testing"
)

func TestMobileTranscodingURL(t *testing.T) {
	got, err := mobileTranscodingURL("https://drive-m.quark.cn/1/clouddrive/time?ut=u&kps=k&sign=s&vcode=v&device_model=test")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "drive-m.quark.cn" || u.Path != "/1/clouddrive/file/v2/play" {
		t.Fatalf("rewritten endpoint = %s%s", u.Host, u.Path)
	}
	for key, want := range map[string]string{"ut": "u", "kps": "k", "sign": "s", "vcode": "v", "device_model": "test"} {
		if got := u.Query().Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}
}

func TestMobileTranscodingURLRejectsIncompleteOrUntrustedURL(t *testing.T) {
	tests := []string{
		"http://drive-m.quark.cn/1/clouddrive/time?kps=k&sign=s&vcode=v&ut=u",
		"https://drive.quark.cn/1/clouddrive/time?kps=k&sign=s&vcode=v&ut=u",
		"https://user@drive-m.quark.cn/1/clouddrive/time?kps=k&sign=s&vcode=v&ut=u",
		"https://drive-m.quark.cn:443/1/clouddrive/time?kps=k&sign=s&vcode=v&ut=u",
		"https://drive-m.quark.cn/1/clouddrive/time?kps=k&sign=s&vcode=v",
	}
	for _, input := range tests {
		if _, err := mobileTranscodingURL(input); err == nil {
			t.Errorf("mobileTranscodingURL(%q) unexpectedly succeeded", input)
		}
	}
}
