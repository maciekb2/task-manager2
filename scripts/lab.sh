#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROFILE=systems-lab
NS=systems-lab
export KUBECONFIG="$ROOT/.state/kubeconfig"
MK="$ROOT/.tools/minikube-linux-amd64"
KC="$ROOT/.tools/kubectl"
MINIKUBE_VERSION=v1.39.0
KUBERNETES_VERSION=v1.37.2
PORT="${LAB_PORT:-18085}"
mkdir -p "$ROOT/.tools" "$ROOT/.state"
chmod 700 "$ROOT/.state"
k() { "$KC" --kubeconfig "$KUBECONFIG" --context "$PROFILE" "$@"; }
download() {
  local url="$1" target="$2"
  curl --fail --location --retry 3 --silent --show-error "$url" -o "$target.tmp"
  mv "$target.tmp" "$target"
}
tools() {
  for tool in docker curl python3 sha256sum; do command -v "$tool" >/dev/null || { echo "Required: $tool" >&2; exit 1; }; done
  [[ "$(uname -s)/$(uname -m)" == "Linux/x86_64" ]] || { echo "Bootstrap currently supports Linux amd64." >&2; exit 1; }
  docker info >/dev/null
  if [[ ! -x "$MK" ]]; then
    download "https://github.com/kubernetes/minikube/releases/download/$MINIKUBE_VERSION/minikube-linux-amd64" "$MK"
    download "https://github.com/kubernetes/minikube/releases/download/$MINIKUBE_VERSION/minikube-linux-amd64.sha256" "$MK.sha256"
    echo "$(awk '{print $1}' "$MK.sha256")  $MK" | sha256sum -c -
    chmod 755 "$MK"
  fi
  if [[ ! -x "$KC" ]]; then
    download "https://dl.k8s.io/release/$KUBERNETES_VERSION/bin/linux/amd64/kubectl" "$KC"
    download "https://dl.k8s.io/release/$KUBERNETES_VERSION/bin/linux/amd64/kubectl.sha256" "$KC.sha256"
    echo "$(cat "$KC.sha256")  $KC" | sha256sum -c -
    chmod 755 "$KC"
  fi
}
stop_forward() {
  python3 "$ROOT/scripts/forward.py" stop
}
forward() {
  python3 "$ROOT/scripts/forward.py" start "$PORT"
}
deploy() {
  k get node "$PROFILE" >/dev/null
  export LAB_VERSION
  LAB_VERSION="$(python3 "$ROOT/scripts/source-version.py")"
  export LAB_IMAGE="systems-lab:$LAB_VERSION"
  if ! docker image inspect "$LAB_IMAGE" >/dev/null 2>&1; then docker build --build-arg "VERSION=$LAB_VERSION" -t "$LAB_IMAGE" "$ROOT"; fi
  if ! docker image inspect "$LAB_IMAGE-demo-fault" >/dev/null 2>&1; then docker build --build-arg "VERSION=$LAB_VERSION" --build-arg DEMO_FAULT=echo503 -t "$LAB_IMAGE-demo-fault" "$ROOT"; fi
  "$MK" -p "$PROFILE" image load "$LAB_IMAGE"
  "$MK" -p "$PROFILE" image load "$LAB_IMAGE-demo-fault"
  k create namespace "$NS" --dry-run=client -o yaml | k apply -f -
  if ! k -n "$NS" get secret lab-nats >/dev/null 2>&1; then
    python3 - "$ROOT/.state/nats-token" <<'PY'
import os, secrets, sys
os.umask(0o077)
with open(sys.argv[1], 'w') as f: f.write(secrets.token_hex(32))
PY
    k -n "$NS" create secret generic lab-nats --from-file="token=$ROOT/.state/nats-token" >/dev/null
    rm "$ROOT/.state/nats-token"
  fi
  if ! k -n "$NS" get configmap lab-config >/dev/null 2>&1; then
    k -n "$NS" create configmap lab-config --from-literal='lab.json={"rps":0,"concurrency":4,"delay_ms":40,"error_percent":0,"cpu_ms":0,"run_until":0}' >/dev/null
  fi
  python3 "$ROOT/deploy/render.py" > "$ROOT/.state/manifests.json"
  k apply -f "$ROOT/.state/manifests.json"
  k -n "$NS" rollout status deployment/nats --timeout=180s
  for role in jaeger control observer web generator gateway validator enricher echo consumer result-store; do
    k -n "$NS" rollout status "deployment/$role" --timeout=180s
  done
  printf '%s\n' "$LAB_IMAGE" > "$ROOT/.state/image"
  forward
}
case "${1:-up}" in
  up)
    tools
    "$MK" start -p "$PROFILE" --driver=docker --container-runtime=containerd \
      --cpus="${LAB_CPUS:-4}" --memory="${LAB_MEMORY:-6144}" --disk-size=16g \
      --kubernetes-version="$KUBERNETES_VERSION" --subnet=192.168.56.0/24 \
      --service-cluster-ip-range=10.110.0.0/16 --keep-context --embed-certs
    "$MK" -p "$PROFILE" addons enable metrics-server
    deploy
    ;;
  deploy) tools; deploy ;;
  open) forward ;;
  status) "$MK" -p "$PROFILE" status; k -n "$NS" get deployments,pods,jobs,cronjobs; printf '\nPanel: http://127.0.0.1:%s\n' "$PORT" ;;
  stop) stop_forward; "$MK" -p "$PROFILE" stop ;;
  down)
    [[ "${LAB_CONFIRM_DELETE:-}" == "$PROFILE" ]] || { echo 'Deletes ONLY the lab profile and its queue/results data. Run LAB_CONFIRM_DELETE=systems-lab make down.' >&2; exit 1; }
    stop_forward; "$MK" -p "$PROFILE" delete
    ;;
  logs) k -n "$NS" logs deployment/control --tail=80 ;;
  *) echo "Usage: $0 {up|deploy|open|status|stop|down|logs}" >&2; exit 1 ;;
esac
