package lab

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Set only while building the deliberately faulty local demo image. A client
// cannot select a fault variant, command, URL or arbitrary image through the API.
var demoFault string

func releaseImage(candidate bool) (string, error) {
	key := "LAB_IMAGE_CURRENT"
	if candidate {
		key = "LAB_IMAGE_CANDIDATE"
	}
	image := env(key, "")
	if !strings.HasPrefix(image, "systems-lab:") || strings.ContainsAny(image, " /\\\n\r\t") {
		return "", fmt.Errorf("lokalne obrazy demo nie są skonfigurowane")
	}
	return image, nil
}
func (k *Kube) releaseEcho(candidate bool) (string, error) {
	image, err := releaseImage(candidate)
	if err != nil {
		return "", err
	}
	deployment, err := k.object(apps("deployments/echo"))
	if err != nil {
		return "", err
	}
	if textField(deployment, "metadata", "labels", "app.kubernetes.io/part-of") != "systems-lab" {
		return "", fmt.Errorf("zasób spoza laboratorium")
	}
	// Preserve the full named-container list; merge-patch arrays replace rather than merge.
	containers := itemsField(deployment, "spec", "template", "spec", "containers")
	found := false
	for _, c := range containers {
		if textField(c, "name") == "echo" {
			c.(map[string]any)["image"] = image
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("brak kontenera echo")
	}
	_, err = k.request("PATCH", apps("deployments/echo"), Object{"spec": Object{"template": Object{"spec": Object{"containers": containers}}}})
	return image, err
}
func (a *App) changeRelease(candidate bool) (string, error) {
	c := a.getConfig()
	if candidate {
		// Persist recovery intent before changing the deployment, including across a
		// control restart. The interval bounds this intentionally broken business path.
		c.ReleaseUntil = unixSeconds(time.Now().Add(60 * time.Second))
		if err := a.kube.saveConfig(c); err != nil {
			return "", err
		}
		a.mu.Lock()
		a.config = c
		a.mu.Unlock()
	}
	image, err := a.kube.releaseEcho(candidate)
	if err != nil {
		return "", err
	}
	if !candidate {
		c.ReleaseUntil = 0
		if err := a.kube.saveConfig(c); err != nil {
			return "", err
		}
		a.mu.Lock()
		a.config = c
		a.mu.Unlock()
	}
	a.emit(Event{From: "control", To: "echo", Kind: "action", Message: "Wersja echo: " + image})
	return image, nil
}
func (a *App) recoverRelease(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			c := a.getConfig()
			if c.ReleaseUntil == 0 || unixSeconds(time.Now()) < c.ReleaseUntil {
				continue
			}
			a.actionMu.Lock()
			c = a.getConfig()
			if c.ReleaseUntil > 0 && unixSeconds(time.Now()) >= c.ReleaseUntil {
				if _, err := a.changeRelease(false); err != nil {
					fmt.Println("release recovery:", err)
				}
			}
			a.actionMu.Unlock()
		}
	}
}
