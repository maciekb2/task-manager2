package lab

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type Object map[string]any
type Kube struct {
	client *http.Client
	base   string
}

func newKube() (*Kube, error) {
	ca, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, fmt.Errorf("invalid Kubernetes CA")
	}
	return &Kube{base: "https://kubernetes.default.svc", client: &http.Client{Timeout: 8 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}}, nil
}
func (k *Kube) request(method, path string, body any) ([]byte, error) {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, k.base+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	if method == "PATCH" {
		req.Header.Set("Content-Type", "application/merge-patch+json")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Kubernetes %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return data, nil
}
func (k *Kube) object(path string) (Object, error) {
	b, e := k.request("GET", path, nil)
	var o Object
	if e == nil {
		e = json.Unmarshal(b, &o)
	}
	return o, e
}
func (k *Kube) list(path string, managed bool) ([]Object, error) {
	if managed {
		path += "?labelSelector=" + url.QueryEscape(ManagedLabel)
	}
	b, e := k.request("GET", path, nil)
	var v struct {
		Items []Object `json:"items"`
	}
	if e == nil {
		e = json.Unmarshal(b, &v)
	}
	if v.Items == nil {
		v.Items = []Object{}
	}
	return v.Items, e
}
func field(o any, keys ...string) any {
	for _, key := range keys {
		switch v := o.(type) {
		case Object:
			o = v[key]
		case map[string]any:
			o = v[key]
		default:
			return nil
		}
	}
	return o
}
func textField(o any, keys ...string) string { s, _ := field(o, keys...).(string); return s }
func numField(o any, keys ...string) float64 { n, _ := field(o, keys...).(float64); return n }
func itemsField(o any, keys ...string) []any { a, _ := field(o, keys...).([]any); return a }
func core(resource string) string            { return "/api/v1/namespaces/" + Namespace + "/" + resource }
func apps(resource string) string            { return "/apis/apps/v1/namespaces/" + Namespace + "/" + resource }
func batch(resource string) string           { return "/apis/batch/v1/namespaces/" + Namespace + "/" + resource }
func (k *Kube) loadConfig() (Config, error) {
	o, e := k.object(core("configmaps/lab-config"))
	if e != nil {
		return Config{}, e
	}
	var c Config
	e = json.Unmarshal([]byte(textField(o, "data", "lab.json")), &c)
	return c, e
}
func (k *Kube) saveConfig(c Config) error {
	raw, _ := json.Marshal(c)
	_, err := k.request("PATCH", core("configmaps/lab-config"), Object{"data": Object{"lab.json": string(raw)}})
	return err
}

func (k *Kube) snapshot() Object {
	result := Object{"namespace": Namespace, "at": time.Now().UTC(), "errors": []string{}}
	errors := []string{}
	for key, path := range map[string]string{"pods": core("pods"), "services": core("services"), "deployments": apps("deployments"), "jobs": batch("jobs"), "cronjobs": batch("cronjobs"), "events": core("events"), "nodes": "/api/v1/nodes"} {
		objects, err := k.list(path, key != "events" && key != "nodes")
		if err != nil {
			errors = append(errors, err.Error())
		}
		views := []Object{}
		for _, o := range objects {
			v := Object{"name": textField(o, "metadata", "name"), "component": textField(o, "metadata", "labels", "app.kubernetes.io/component"), "created": field(o, "metadata", "creationTimestamp")}
			switch key {
			case "pods":
				v["phase"] = textField(o, "status", "phase")
				v["ip"] = textField(o, "status", "podIP")
				v["node"] = textField(o, "spec", "nodeName")
				v["restarts"] = 0
				ready := 0
				restarts := 0
				for _, c := range itemsField(o, "status", "containerStatuses") {
					if field(c, "ready") == true {
						ready++
					}
					restarts += int(numField(c, "restartCount"))
				}
				v["ready"] = ready
				v["restarts"] = restarts
				v["containers"] = len(itemsField(o, "spec", "containers"))
				images := []string{}
				for _, c := range itemsField(o, "spec", "containers") {
					images = append(images, textField(c, "image"))
				}
				v["images"] = images
				v["owners"] = field(o, "metadata", "ownerReferences")
			case "deployments":
				v["replicas"] = numField(o, "spec", "replicas")
				v["ready"] = numField(o, "status", "readyReplicas")
				v["selector"] = field(o, "spec", "selector", "matchLabels")
				v["dependencies"] = strings.Split(textField(o, "metadata", "annotations", "lab.systems/dependencies"), ",")
				v["description"] = textField(o, "metadata", "annotations", "lab.systems/description")
			case "services":
				v["ip"] = textField(o, "spec", "clusterIP")
				v["selector"] = field(o, "spec", "selector")
				v["ports"] = field(o, "spec", "ports")
			case "nodes":
				v["capacity"] = field(o, "status", "capacity")
				v["allocatable"] = field(o, "status", "allocatable")
				v["conditions"] = field(o, "status", "conditions")
				v["version"] = textField(o, "status", "nodeInfo", "kubeletVersion")
			case "jobs":
				v["active"] = numField(o, "status", "active")
				v["succeeded"] = numField(o, "status", "succeeded")
				v["failed"] = numField(o, "status", "failed")
				v["conditions"] = field(o, "status", "conditions")
			case "cronjobs":
				v["schedule"] = textField(o, "spec", "schedule")
				v["suspended"] = field(o, "spec", "suspend")
				v["last_schedule"] = field(o, "status", "lastScheduleTime")
			case "events":
				v["reason"] = textField(o, "reason")
				v["message"] = textField(o, "message")
				v["type"] = textField(o, "type")
				v["object"] = field(o, "involvedObject")
				v["time"] = field(o, "lastTimestamp")
			}
			views = append(views, v)
		}
		sort.Slice(views, func(i, j int) bool { return fmt.Sprint(views[i]["name"]) < fmt.Sprint(views[j]["name"]) })
		if key == "events" {
			sort.Slice(views, func(i, j int) bool { return fmt.Sprint(views[i]["time"]) > fmt.Sprint(views[j]["time"]) })
			if len(views) > 40 {
				views = views[:40]
			}
		}
		result[key] = views
	}
	if o, e := k.object("/version"); e == nil {
		result["version"] = o["gitVersion"]
	} else {
		errors = append(errors, e.Error())
	}
	if o, e := k.object("/apis/metrics.k8s.io/v1beta1/namespaces/" + Namespace + "/pods"); e == nil {
		result["usage"] = o["items"]
	} else {
		result["usage_error"] = "Metrics Server jeszcze niedostępny"
	}
	result["errors"] = errors
	return result
}

var mutable = map[string]bool{"echo": true, "gateway": true, "validator": true, "enricher": true, "result-store": true, "generator": true, "consumer": true}

func (k *Kube) scale(name string, replicas int) error {
	if !mutable[name] || replicas < 0 || replicas > 5 {
		return fmt.Errorf("dozwolone tylko workloady laboratorium i 0–5 replik")
	}
	o, err := k.object(apps("deployments/" + name))
	if err != nil {
		return err
	}
	if textField(o, "metadata", "labels", "app.kubernetes.io/part-of") != "systems-lab" {
		return fmt.Errorf("zasób spoza laboratorium")
	}
	_, err = k.request("PATCH", apps("deployments/"+name+"/scale"), Object{"spec": Object{"replicas": replicas}})
	return err
}
func (k *Kube) restartPod(name string) error {
	if strings.ContainsAny(name, "/?#%") || name == "" {
		return fmt.Errorf("nieprawidłowa nazwa poda")
	}
	o, e := k.object(core("pods/" + name))
	if e != nil {
		return e
	}
	if textField(o, "metadata", "labels", "app.kubernetes.io/part-of") != "systems-lab" || !mutable[textField(o, "metadata", "labels", "app.kubernetes.io/component")] {
		return fmt.Errorf("restart tylko aplikacji demonstracyjnych")
	}
	_, e = k.request("DELETE", core("pods/"+name), Object{"kind": "DeleteOptions", "apiVersion": "v1"})
	return e
}
func (k *Kube) createJob(failure bool) (string, error) {
	cron, e := k.object(batch("cronjobs/lab-tick"))
	if e != nil {
		return "", e
	}
	spec := field(cron, "spec", "jobTemplate", "spec")
	raw, _ := json.Marshal(spec)
	var jobSpec Object
	_ = json.Unmarshal(raw, &jobSpec)
	if failure {
		containers := itemsField(jobSpec, "template", "spec", "containers")
		if len(containers) > 0 {
			c := containers[0].(map[string]any)
			c["env"] = append(itemsField(c, "env"), Object{"name": "JOB_FAIL", "value": "true"})
		}
	}
	name := "lab-job-" + id()[:10]
	job := Object{"apiVersion": "batch/v1", "kind": "Job", "metadata": Object{"name": name, "labels": Object{"app.kubernetes.io/part-of": "systems-lab", "app.kubernetes.io/component": "job"}}, "spec": jobSpec}
	_, e = k.request("POST", batch("jobs"), job)
	return name, e
}
func (k *Kube) cron(suspended bool, schedule string) error {
	if schedule != "* * * * *" && schedule != "*/5 * * * *" {
		return fmt.Errorf("wybierz interwał 1 lub 5 minut")
	}
	_, e := k.request("PATCH", batch("cronjobs/lab-tick"), Object{"spec": Object{"suspend": suspended, "schedule": schedule}})
	return e
}
