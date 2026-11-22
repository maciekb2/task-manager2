#!/usr/bin/env python3
"""Render the complete local lab as a Kubernetes List. No Helm/Python packages."""
import hashlib
import json
import os
from pathlib import Path

NS = "systems-lab"
IMAGE = os.environ.get("LAB_IMAGE", "systems-lab:dev")
VERSION = os.environ.get("LAB_VERSION", "dev")
LABELS = {"app.kubernetes.io/part-of": "systems-lab"}
items = []

def add(kind, name, spec=None, api="v1", **extra):
    obj = {"apiVersion": api, "kind": kind,
           "metadata": {"name": name, "namespace": NS, "labels": dict(LABELS)}}
    if kind in ("Namespace", "ClusterRole", "ClusterRoleBinding"):
        obj["metadata"].pop("namespace")
    if spec is not None:
        obj["spec"] = spec
    obj.update(extra)
    items.append(obj)
    return obj

add("Namespace", NS)
for name in ("control", "observer", "workload"):
    add("ServiceAccount", name, automountServiceAccountToken=False)

read_rules = [
    {"apiGroups": [""], "resources": ["pods", "services", "events"], "verbs": ["get", "list"]},
    {"apiGroups": ["apps"], "resources": ["deployments"], "verbs": ["get", "list"]},
    {"apiGroups": ["batch"], "resources": ["jobs", "cronjobs"], "verbs": ["get", "list"]},
    {"apiGroups": ["metrics.k8s.io"], "resources": ["pods"], "verbs": ["get", "list"]},
]
add("Role", "observer", api="rbac.authorization.k8s.io/v1", rules=read_rules)
add("RoleBinding", "observer", api="rbac.authorization.k8s.io/v1",
    roleRef={"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "observer"},
    subjects=[{"kind": "ServiceAccount", "name": "observer", "namespace": NS}])
add("ClusterRole", "systems-lab-observer", api="rbac.authorization.k8s.io/v1",
    rules=[{"apiGroups": [""], "resources": ["nodes"], "verbs": ["get", "list"]},
           {"apiGroups": ["metrics.k8s.io"], "resources": ["nodes"], "verbs": ["get", "list"]}])
add("ClusterRoleBinding", "systems-lab-observer", api="rbac.authorization.k8s.io/v1",
    roleRef={"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "systems-lab-observer"},
    subjects=[{"kind": "ServiceAccount", "name": "observer", "namespace": NS}])

scalable = ["gateway", "validator", "enricher", "echo", "generator", "consumer", "result-store"]
control_rules = [
    {"apiGroups": [""], "resources": ["configmaps"], "resourceNames": ["lab-config"], "verbs": ["get", "patch"]},
    {"apiGroups": [""], "resources": ["pods"], "verbs": ["get", "delete"]},
    {"apiGroups": [""], "resources": ["pods/log"], "verbs": ["get"]},
    {"apiGroups": ["apps"], "resources": ["deployments", "deployments/scale"], "resourceNames": scalable, "verbs": ["get", "patch"]},
    {"apiGroups": ["batch"], "resources": ["jobs"], "verbs": ["create"]},
    {"apiGroups": ["batch"], "resources": ["cronjobs"], "resourceNames": ["lab-tick"], "verbs": ["get", "patch"]},
]
add("Role", "control", api="rbac.authorization.k8s.io/v1", rules=control_rules)
add("RoleBinding", "control", api="rbac.authorization.k8s.io/v1",
    roleRef={"apiGroup": "rbac.authorization.k8s.io", "kind": "Role", "name": "control"},
    subjects=[{"kind": "ServiceAccount", "name": "control", "namespace": NS}])

nats_config = add("ConfigMap", "nats-config", data={"nats.conf": """port: 4222
http_port: 8222
authorization { token: $NATS_TOKEN }
jetstream { store_dir: /data/jetstream, max_memory_store: 64MB, max_file_store: 512MB }
"""})
add("PersistentVolumeClaim", "nats-data", {"accessModes": ["ReadWriteOnce"], "resources": {"requests": {"storage": "1Gi"}}})
labels = dict(LABELS, **{"app.kubernetes.io/component": "nats"})
broker = add("Deployment", "nats", {
    "replicas": 1, "strategy": {"type": "Recreate"}, "selector": {"matchLabels": labels},
    "template": {"metadata": {"labels": labels}, "spec": {
        "serviceAccountName": "workload", "automountServiceAccountToken": False,
        "securityContext": {"runAsUser": 1000, "runAsGroup": 1000, "fsGroup": 1000, "runAsNonRoot": True, "seccompProfile": {"type": "RuntimeDefault"}},
        "containers": [{"name": "nats", "image": "nats:2.15.3-alpine", "args": ["-c", "/etc/nats/nats.conf"],
            "env": [{"name": "NATS_TOKEN", "valueFrom": {"secretKeyRef": {"name": "lab-nats", "key": "token"}}}],
            "ports": [{"containerPort": 4222}, {"containerPort": 8222}],
            "resources": {"requests": {"cpu": "50m", "memory": "128Mi"}, "limits": {"cpu": "1", "memory": "512Mi"}},
            "securityContext": {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]}},
            "readinessProbe": {"httpGet": {"path": "/healthz", "port": 8222}, "periodSeconds": 3},
            "volumeMounts": [{"name": "config", "mountPath": "/etc/nats", "readOnly": True}, {"name": "data", "mountPath": "/data"}]}],
        "volumes": [{"name": "config", "configMap": {"name": "nats-config"}}, {"name": "data", "persistentVolumeClaim": {"claimName": "nats-data"}}]}}}, api="apps/v1")
broker["spec"]["template"]["metadata"]["annotations"] = {"lab.systems/config-sha256": hashlib.sha256(nats_config["data"]["nats.conf"].encode()).hexdigest()}
broker["metadata"]["labels"].update(labels)
broker["metadata"]["annotations"] = {"lab.systems/description": "JetStream: trwałe kolejki, zdarzenia, wyniki i DLQ"}
add("Service", "nats", {"selector": labels, "ports": [{"name": "client", "port": 4222, "targetPort": 4222}]})
items[-1]["metadata"]["labels"].update(labels)

add("ConfigMap", "jaeger-config", data={"config.yaml": Path(__file__).with_name("jaeger.yaml").read_text()})
labels = dict(LABELS, **{"app.kubernetes.io/component": "jaeger"})
jaeger = add("Deployment", "jaeger", {"replicas": 1, "selector": {"matchLabels": labels},
    "template": {"metadata": {"labels": labels}, "spec": {"serviceAccountName": "workload", "automountServiceAccountToken": False,
        "securityContext": {"runAsUser": 10001, "runAsGroup": 10001, "runAsNonRoot": True, "seccompProfile": {"type": "RuntimeDefault"}},
        "containers": [{"name": "jaeger", "image": "cr.jaegertracing.io/jaegertracing/jaeger:2.23.0", "args": ["--config", "/config/config.yaml"],
            "ports": [{"containerPort": 16686}, {"containerPort": 4318}],
            "securityContext": {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]}},
            "resources": {"requests": {"cpu": "50m", "memory": "128Mi"}, "limits": {"cpu": "1", "memory": "512Mi"}},
            "readinessProbe": {"httpGet": {"path": "/tracing/", "port": 16686}, "periodSeconds": 3},
            "volumeMounts": [{"name": "config", "mountPath": "/config", "readOnly": True}]}],
        "volumes": [{"name": "config", "configMap": {"name": "jaeger-config"}}]}}}, api="apps/v1")
jaeger["spec"]["template"]["metadata"]["annotations"] = {"lab.systems/config-sha256": hashlib.sha256(Path(__file__).with_name("jaeger.yaml").read_bytes()).hexdigest()}
jaeger["metadata"]["labels"].update(labels)
jaeger["metadata"]["annotations"] = {"lab.systems/description": "OpenTelemetry: standardowe ślady i czasy spanów; lokalna pamięć 5000 trace"}
add("Service", "jaeger", {"selector": labels, "ports": [{"name": "query", "port": 16686}, {"name": "otlp-http", "port": 4318}]})
items[-1]["metadata"]["labels"].update(labels)

roles = {
    "web": ("control", "Panel WWW i interaktywna mapa laboratorium"),
    "control": ("observer,nats,result-store", "API scenariuszy; ograniczone akcje w namespace laboratorium"),
    "observer": ("", "Odkrywanie klastra przez Kubernetes API; tylko odczyt"),
    "generator": ("gateway,nats,control", "Rzeczywiste żądania HTTP z limitem czasu i współbieżności"),
    "gateway": ("validator,nats", "Punkt wejścia i identyfikator korelacji żądania"),
    "validator": ("enricher,nats", "Walidacja liczby jednostek pracy; błędne żądania odrzuca HTTP 422"),
    "enricher": ("echo,nats", "Wzbogacenie żądania o klasę pracy i identyfikator"),
    "echo": ("control", "Wykonanie pracy: opóźnienie, CPU, kontrolowany błąd HTTP"),
    "consumer": ("nats,gateway", "Konsument JetStream: ACK, ponowienia i dead-letter"),
    "result-store": ("nats", "Idempotentny zapis wyników HTTP w trwałym KV JetStream"),
}
upstream = {"gateway": "validator", "validator": "enricher", "enricher": "echo"}
def pod_spec(role):
    env = [{"name": "ROLE", "value": role}, {"name": "LAB_VERSION", "value": VERSION}]
    if role == "control":
        env += [{"name":"LAB_IMAGE_CURRENT","value":IMAGE},{"name":"LAB_IMAGE_CANDIDATE","value":IMAGE+"-demo-fault"}]
    if role != "web":
        env += [{"name": "NATS_URL", "value": "nats://nats:4222"}, {"name": "NATS_TOKEN", "valueFrom": {"secretKeyRef": {"name": "lab-nats", "key": "token"}}}]
    if role in upstream:
        env.append({"name": "UPSTREAM", "value": "http://" + upstream[role] + ":8080"})
    return {"serviceAccountName": role if role in ("control", "observer") else "workload",
        "automountServiceAccountToken": role in ("control", "observer"),
        "securityContext": {"runAsNonRoot": True, "runAsUser": 10001, "runAsGroup": 10001, "seccompProfile": {"type": "RuntimeDefault"}},
        "terminationGracePeriodSeconds": 25,
        "containers": [{"name": role, "image": IMAGE, "imagePullPolicy": "IfNotPresent", "env": env,
            "ports": [{"containerPort": 8080}],
            "securityContext": {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]}},
            "resources": {"requests": {"cpu": "20m", "memory": "32Mi"}, "limits": {"cpu": "500m", "memory": "256Mi"}},
            "readinessProbe": {"httpGet": {"path": "/healthz", "port": 8080}, "periodSeconds": 3},
            "livenessProbe": {"httpGet": {"path": "/livez", "port": 8080}, "initialDelaySeconds": 120, "periodSeconds": 10}}]}

for role, (deps, description) in roles.items():
    labels = dict(LABELS, **{"app.kubernetes.io/component": role})
    obj = add("Deployment", role, {"replicas": 2 if role == "echo" else 1,
        "selector": {"matchLabels": labels}, "template": {"metadata": {"labels": labels}, "spec": pod_spec(role)}}, api="apps/v1")
    obj["metadata"]["labels"].update(labels)
    obj["metadata"]["annotations"] = {"lab.systems/dependencies": deps, "lab.systems/description": description}
    obj = add("Service", role, {"selector": labels, "ports": [{"name": "http", "port": 8080, "targetPort": 8080}]})
    obj["metadata"]["labels"].update(labels)

job_pod = pod_spec("job")
job_pod["restartPolicy"] = "Never"
for probe in ("readinessProbe", "livenessProbe"):
    job_pod["containers"][0].pop(probe)
add("CronJob", "lab-tick", {"schedule": "* * * * *", "suspend": True, "concurrencyPolicy": "Forbid",
    "successfulJobsHistoryLimit": 2, "failedJobsHistoryLimit": 2, "startingDeadlineSeconds": 30,
    "jobTemplate": {"metadata": {"labels": dict(LABELS)}, "spec": {
        "backoffLimit": 2, "activeDeadlineSeconds": 90, "ttlSecondsAfterFinished": 600,
        "template": {"metadata": {"labels": dict(LABELS, **{"app.kubernetes.io/component": "job"})}, "spec": job_pod}}}}, api="batch/v1")

print(json.dumps({"apiVersion": "v1", "kind": "List", "items": items}, indent=2))
