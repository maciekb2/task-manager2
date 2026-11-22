# Systems Lab

This is a public, synthetic-data Kubernetes teaching project. Keep all runnable
code, cluster manifests, bootstrap and examples in this repository.

* Use only profile/context/namespace `systems-lab` and `.state/kubeconfig` for lab
  mutations. Never rely on the operator's current kubeconfig context.
* Preserve `legacy/v1` and predecessor Git history. Do not silently restore legacy
  modules to the active build or archive predecessor repositories.
* `make up` must remain sufficient to create/reconcile the local lab. No shared
  private platform, cloud credential, workstation path or private endpoint may be
  required at runtime or committed to public docs.
* UI data must come from Kubernetes/actual events. Clearly distinguish declared
  dependency edges, observed traffic, configured rates and actual measurements.
* Mutations stay within bounded lab experiments. Keep token/origin checks, RBAC
  scope, resource/time limits and fixed HTTP targets. Do not display Secrets.
* New behavior: run relevant `make test` checks and actual cluster acceptance with
  `make smoke` as appropriate. Reuse unchanged successful evidence. Do not claim
  E2E or CI/CD from compilation alone.
* `.state`, `.tools`, output and kubeconfigs are ignored and must not be published.
