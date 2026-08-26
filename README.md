# dyson-sphere

A Go service platform and paved road for [consu](#what-consu-is), a self-hosted Kubernetes
cluster.

Right now it is **one reference HTTP service** that is production-shaped from the first commit:
health probes, graceful shutdown, distributed tracing, RED metrics, a distroless image, and a
CI pipeline that is deliberately not allowed to touch the cluster.

Eventually it will be a generator that emits that service on demand, plus a check that fails
your build when a service drifts off the road. **That part is not written yet** — see
[Status](#status). This README describes what is in the repository today.

---

## Why this exists

Most scaffolding tools generate a project and walk away. Spring Initializr, Cookiecutter, Yeoman
— they all hand you a correct starting point and then assume you stay disciplined.

I don't think that assumption survives contact with a deadline. My position, written down before
this repo existed:

> You can't rely on developer tendencies or discipline. My preference is having systems in place
> to enforce hard requirements. Built requirements for enforcement force the gate — it shifts
> from a documented talking point to a system success requirement.

So the intended product is **generate + verify**. A generated service starts compliant, and CI
refuses to merge it once it stops being compliant. Generation alone is a template. The gate is
the point.

The reference service in this repo is the thing the generator will eventually emit. Building it
by hand first is deliberate: I want the target to be something I have actually run, not something
I imagined.

---

## Status

| Piece | State |
|---|---|
| Reference Go service — probes, graceful shutdown | **Built** |
| OpenTelemetry tracing → Tempo | **Built** |
| Prometheus RED metrics | **Built** |
| Distroless container image | **Built** |
| Service pipeline — test, build, scan, propose deploy | **Built** |
| Generator CLI (`dyson new`) | Not started |
| Compliance gate (`dyson verify`) | Not started |
| Additional language templates | Not started |

This is an early, actively-built project. It is public because the reasoning is worth reading,
not because it is finished.

---

## What is actually interesting here

Three decisions, if you only read one section.

### 1. The pipeline is denied the credentials to deploy

The service pipeline builds, tests, scans, and publishes one service across **no environments**.
It never learns a hostname, a namespace, or a cluster credential. When it finishes, it opens a
merge request against a separate GitOps repository proposing a new image pin — and stops.

Resolving what actually runs where is the environment pipeline's job, in `consu-config`, where
Argo CD reconciles it.

The reason is not tidiness. A pipeline that can deploy is a pipeline that can be made to deploy
anything, by anyone who can land a commit. Splitting build authority from deploy authority means
compromising the service repository does not hand you the cluster.

### 2. Metrics are labelled on route patterns, never raw paths

```go
// Label on the ROUTE PATTERN ("/api/hello"), never the raw path.
```

Labelling a Prometheus counter with the raw request path is the classic cardinality explosion:
one time series per unique URL, forever, until Prometheus falls over. `/users/1`, `/users/2`,
`/users/3` are three series that should have been one.

### 3. The metrics registry is private, not global

`prometheus.DefaultRegisterer` is a package-level singleton. Register the same collector twice —
which two tests constructing the same service will do — and the second one panics. Carrying a
private `*prometheus.Registry` on the `Metrics` struct means a test can build one, assert on it,
and throw it away.

Small thing. Costs an afternoon to find the first time.

---

## Layout

```
cmd/server/            entrypoint: wiring, signal handling, shutdown ordering
internal/server/       HTTP handlers, routing, readiness and liveness
internal/observability/
    tracing.go         OpenTelemetry tracer provider, OTLP export to Tempo
    metrics.go         RED signals — rate, errors, duration — on a private registry
    middleware.go      one status-recording wrapper, shared by logging and metrics
    resource.go        OTel resource attributes (service name, version, commit)
internal/build/        version and commit, injected at link time
Dockerfile             multi-stage, distroless runtime, CGO disabled
.gitlab-ci.yml         test → build → scan → propose
```

## Running it

```bash
make run          # build and run locally
make test         # go test -race -cover ./...
curl localhost:8080/healthz
curl localhost:8080/readyz
curl localhost:8080/metrics
```

Tracing exports over OTLP. Without a collector reachable it degrades quietly rather than
refusing to start.

## The pipeline

```
test    go vet · gofmt check (fails on unformatted files) · go test -race -cover
build   multi-stage distroless image, tagged with the commit SHA — never `latest`
scan    trivy, HIGH and CRITICAL
propose merge request against consu-config bumping the Kustomize image pin
```

Image tags are immutable and commit-pinned. `latest` is never deployed, so "which build is in
production" always has an answer.

## What consu is

A self-hosted k3s cluster on Proxmox, provisioned with Terraform, running Argo CD,
kube-prometheus-stack, Tempo, Authentik, Falco, and Cilium. `consu-config` is its GitOps
repository. This project is the on-ramp to a road that already exists — it is not building the
cluster.

---

## A note on the module path

The canonical repository is on GitLab (`gitlab.com/navetoocool/dyson-sphere`) so it can use
consu's CI runners and dev cluster, and the Go module path reflects that. This GitHub copy is a
push mirror kept in sync for readability. Read it here; it is built there.

---

Built by [Evan Thompson](https://github.com/evanpthompson).
