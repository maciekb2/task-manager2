package lab

import "testing"

func TestReleaseOnlySelectsConfiguredLocalImages(t *testing.T) {
	t.Setenv("LAB_IMAGE_CURRENT", "systems-lab:source123")
	t.Setenv("LAB_IMAGE_CANDIDATE", "systems-lab:source123-demo-fault")
	if image, err := releaseImage(false); err != nil || image != "systems-lab:source123" {
		t.Fatal(image, err)
	}
	if image, err := releaseImage(true); err != nil || image != "systems-lab:source123-demo-fault" {
		t.Fatal(image, err)
	}
	for _, invalid := range []string{"", "other/app:latest", "systems-lab:tag\nextra", "systems-lab:tag/path"} {
		t.Setenv("LAB_IMAGE_CANDIDATE", invalid)
		if _, err := releaseImage(true); err == nil {
			t.Fatal("invalid release target accepted", invalid)
		}
	}
}
