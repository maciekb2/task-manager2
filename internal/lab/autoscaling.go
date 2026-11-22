package lab

import (
	"context"
	"fmt"
	"time"
)

func autoscalingPath() string {
	return "/apis/autoscaling/v2/namespaces/" + Namespace + "/horizontalpodautoscalers"
}
func (k *Kube) echoAutoscaler(enabled bool) error {
	objects, err := k.list(autoscalingPath(), true)
	if err != nil {
		return err
	}
	exists := false
	for _, o := range objects {
		if textField(o, "metadata", "name") == "echo" {
			exists = true
		}
	}
	if !enabled {
		if !exists {
			return nil
		}
		_, err = k.request("DELETE", autoscalingPath()+"/echo", Object{})
		return err
	}
	spec := Object{"scaleTargetRef": Object{"apiVersion": "apps/v1", "kind": "Deployment", "name": "echo"}, "minReplicas": 1, "maxReplicas": 5,
		"metrics":  []Object{{"type": "Resource", "resource": Object{"name": "cpu", "target": Object{"type": "AverageValue", "averageValue": "200m"}}}},
		"behavior": Object{"scaleUp": Object{"stabilizationWindowSeconds": 0, "policies": []Object{{"type": "Pods", "value": 2, "periodSeconds": 15}}}, "scaleDown": Object{"stabilizationWindowSeconds": 30, "policies": []Object{{"type": "Pods", "value": 1, "periodSeconds": 15}}}}}
	if exists {
		_, err = k.request("PATCH", autoscalingPath()+"/echo", Object{"spec": spec})
	} else {
		_, err = k.request("POST", autoscalingPath(), Object{"apiVersion": "autoscaling/v2", "kind": "HorizontalPodAutoscaler", "metadata": Object{"name": "echo", "namespace": Namespace, "labels": Object{"app.kubernetes.io/part-of": "systems-lab", "app.kubernetes.io/component": "echo"}}, "spec": spec})
	}
	return err
}
func (a *App) changeAutoscaling(enabled bool) error {
	c := a.getConfig()
	if enabled {
		if c.RunID != "" {
			return fmt.Errorf("HPA demo jest globalne; najpierw zakończ eksperyment A/B")
		}
		c.AutoscaleUntil = unixSeconds(time.Now().Add(300 * time.Second))
		if err := a.kube.saveConfig(c); err != nil {
			return err
		}
		a.mu.Lock()
		a.config = c
		a.mu.Unlock()
	}
	if err := a.kube.echoAutoscaler(enabled); err != nil {
		return err
	}
	if !enabled {
		if err := a.kube.scale("echo", 2); err != nil {
			return err
		}
		c.AutoscaleUntil = 0
		if err := a.kube.saveConfig(c); err != nil {
			return err
		}
		a.mu.Lock()
		a.config = c
		a.mu.Unlock()
		return nil
	}
	return nil
}
func (a *App) recoverAutoscaling(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			c := a.getConfig()
			if c.AutoscaleUntil == 0 || unixSeconds(time.Now()) < c.AutoscaleUntil {
				continue
			}
			a.actionMu.Lock()
			c = a.getConfig()
			if c.AutoscaleUntil > 0 && unixSeconds(time.Now()) >= c.AutoscaleUntil {
				if err := a.changeAutoscaling(false); err != nil {
					fmt.Println("autoscaling recovery:", err)
				} else {
					a.emit(Event{From: "control", To: "echo", Kind: "action", Message: "HPA demo: koniec limitu 300 s; przywrócono 2 repliki"})
				}
			}
			a.actionMu.Unlock()
		}
	}
}

// One actionMu-protected operation: a subsequent enable cannot interleave with
// reset/scale/configuration halfway through baseline preparation.
func (a *App) startCPUBaseline() error {
	if err := a.resetLab(); err != nil {
		return err
	}
	if err := a.kube.scale("echo", 1); err != nil {
		return err
	}
	c := DefaultConfig()
	c.RPS = 50
	c.Concurrency = 20
	c.DelayMS = 0
	c.CPUMS = 50
	c.DurationSeconds = 180
	c.RunUntil = unixSeconds(time.Now().Add(180 * time.Second))
	if err := a.kube.saveConfig(c); err != nil {
		return err
	}
	a.mu.Lock()
	a.config = c
	a.mu.Unlock()
	return nil
}
