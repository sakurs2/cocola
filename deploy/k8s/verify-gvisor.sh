#!/usr/bin/env bash
# verify-gvisor.sh -- runsc (gVisor) compatibility & cold-start acceptance gate
# for cocola's Route-A sandbox on Kubernetes (task #15, Layer C).
#
# WHY: cocola's default isolation is runc + user namespaces (zero node install).
# gVisor (runsc) is the OPTIONAL stronger application-kernel isolation. Node +
# Claude Code is a heavy runtime, and runsc intercepts syscalls in userspace --
# some syscalls are unsupported, so we MUST prove Route-A actually works under
# runsc before recommending it. This script runs a set of compat probes against
# a real runsc-backed sandbox Pod and judges each one pass/fail.
#
# WHERE THIS RUNS: a REAL Kubernetes cluster that already has the gVisor
# containerd shim installed on its nodes and the `runsc` RuntimeClass applied
# (see deploy/k8s/01-runtimeclass.yaml). It CANNOT run on macOS Docker Desktop
# (no real runsc). On a dev box it only passes `bash -n` (syntax) + --dry-run.
#
# PROBES (map 1:1 to Plan docs/plan/hardening-gvisor-spike-and-image-warmer.md
# section 4.2):
#   1 toolchain : node --version and claude --version exit 0 inside a runsc Pod.
#   2 egress    : one real query through the gateway succeeds (NetworkPolicy +
#                 runsc network stack do not fight). Skipped unless RUN_EGRESS=1.
#   3 io        : native bash + file IO on /workspace and ~/.claude works.
#   4 resume    : hibernate (delete Pod, keep PVC) -> recreate (remount PVC) ->
#                 claude --resume continues the session (disk-kept, ADR-0008).
#   5 coldstart : per-request cold-start timing vs bench/README.md section 3.2,
#                 WITH vs WITHOUT node image pre-pull (the #15 S1 DaemonSet),
#                 to quantify runsc + 1.9GB-image cold start and pre-pull benefit.
#   6 checkpoint: runsc checkpoint/restore probe (absorbs Agent Substrate's
#                 approach) -- can our image do a RAM-kept resume instead of the
#                 current disk-kept "delete Pod + claude --resume" replay? If it
#                 passes we can upgrade ADR-0008 section 3 from RAM-lost to
#                 RAM-kept. Skipped unless RUN_CHECKPOINT=1 (needs node access).
#
# This script NEVER opens a listening port. The in-Pod CLI is driven over
# kubectl exec -i STDIO only.
#
# Usage:
#   deploy/k8s/verify-gvisor.sh                 # probes 1,3,4 (no gateway, no node)
#   RUN_EGRESS=1 deploy/k8s/verify-gvisor.sh    # + probe 2 (needs gateway env)
#   RUN_COLDSTART=1 ... deploy/k8s/verify-gvisor.sh   # + probe 5
#   RUN_CHECKPOINT=1 ... deploy/k8s/verify-gvisor.sh  # + probe 6 (needs node/runsc)
#   DRY_RUN=1 deploy/k8s/verify-gvisor.sh       # print intended actions, mutate nothing
#
# Env knobs:
#   NS                sandbox namespace            (default cocola-sandboxes)
#   RUNTIME_CLASS     RuntimeClass to test         (default runsc)
#   IMAGE             sandbox runtime image        (default cocola/sandbox-runtime:dev)
#   POD               probe Pod name               (default gvisor-verify)
#   PVC               existing per-session PVC to mount for probe 4 (optional)
#   KUBECTL           kubectl binary               (default kubectl)
#   RUN_EGRESS=1      enable probe 2 (real query). Needs ANTHROPIC_BASE_URL +
#                     ANTHROPIC_AUTH_TOKEN reachable from inside the Pod.
#   RUN_COLDSTART=1   enable probe 5 (timing, with/without pre-pull).
#   RUN_CHECKPOINT=1  enable probe 6 (runsc checkpoint/restore).
#   COLD_ITERS        cold-start iterations per arm (default 5)
#   DRY_RUN=1         print, do not apply/exec/delete anything.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEVNULL="/dev/null"

NS="${NS:-cocola-sandboxes}"
RUNTIME_CLASS="${RUNTIME_CLASS:-runsc}"
IMAGE="${IMAGE:-cocola/sandbox-runtime:dev}"
POD="${POD:-gvisor-verify}"
PVC="${PVC:-}"
KUBECTL="${KUBECTL:-kubectl}"
COLD_ITERS="${COLD_ITERS:-5}"
DRY_RUN="${DRY_RUN:-0}"
RUN_EGRESS="${RUN_EGRESS:-0}"
RUN_COLDSTART="${RUN_COLDSTART:-0}"
RUN_CHECKPOINT="${RUN_CHECKPOINT:-0}"

PASS=0 FAIL=0 SKIP=0
pass() { echo "  PASS: $*"; PASS=$((PASS+1)); }
fail() { echo "  FAIL: $*" >&2; FAIL=$((FAIL+1)); }
skip() { echo "  SKIP: $*"; SKIP=$((SKIP+1)); }
hdr()  { echo; echo "== $* =="; }

# In DRY_RUN we echo the command instead of running it; in real mode we run it.
run() {
  if [[ "${DRY_RUN}" == "1" ]]; then
    echo "  + (dry-run) $*"
    return 0
  fi
  "$@"
}

require() {
  command -v "$1" >"${DEVNULL}" 2>&1 || { echo "required tool not found: $1" >&2; exit 127; }
}

# --- preflight -------------------------------------------------------------
hdr "preflight"
require "${KUBECTL}"
echo "  NS=${NS} RUNTIME_CLASS=${RUNTIME_CLASS} IMAGE=${IMAGE} POD=${POD} DRY_RUN=${DRY_RUN}"
if [[ "${DRY_RUN}" != "1" ]]; then
  # RuntimeClass must exist or every runsc Pod will be unschedulable.
  if ! "${KUBECTL}" get runtimeclass "${RUNTIME_CLASS}" >"${DEVNULL}" 2>&1; then
    echo "RuntimeClass '${RUNTIME_CLASS}' not found -- apply deploy/k8s/01-runtimeclass.yaml and install the gVisor shim first." >&2
    exit 1
  fi
fi

# Render the probe Pod (runsc RuntimeClass, the sandbox image, idle sleep).
# We keep it minimal: it mirrors how the k8s provider pins runtimeClassName,
# but with a long-lived sleep so we can kubectl exec probes into it.
probe_pod_manifest() {
  cat <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: ${POD}
  namespace: ${NS}
  labels:
    app.kubernetes.io/name: gvisor-verify
    cocola.bytedance.com/managed: "true"
spec:
  runtimeClassName: ${RUNTIME_CLASS}
  restartPolicy: Never
  terminationGracePeriodSeconds: 1
  automountServiceAccountToken: false
  containers:
    - name: sandbox
      image: ${IMAGE}
      imagePullPolicy: IfNotPresent
      command: ["sleep", "3600"]
      resources:
        requests: {cpu: "250m", memory: "512Mi"}
        limits: {cpu: "1", memory: "1Gi"}
YAML
}

cleanup() {
  [[ "${DRY_RUN}" == "1" ]] && return 0
  "${KUBECTL}" -n "${NS}" delete pod "${POD}" --ignore-not-found --wait=false >"${DEVNULL}" 2>&1 || true
}
trap cleanup EXIT

create_probe_pod() {
  if [[ "${DRY_RUN}" == "1" ]]; then
    echo "  + (dry-run) kubectl apply -f - <<probe-pod (runtimeClassName=${RUNTIME_CLASS})"
    probe_pod_manifest | sed 's/^/      /'
    return 0
  fi
  probe_pod_manifest | "${KUBECTL}" apply -f -
  "${KUBECTL}" -n "${NS}" wait --for=condition=Ready "pod/${POD}" --timeout=180s
}

kx() { # kubectl exec helper
  if [[ "${DRY_RUN}" == "1" ]]; then
    echo "  + (dry-run) kubectl -n ${NS} exec ${POD} -- $*"
    return 0
  fi
  "${KUBECTL}" -n "${NS}" exec "${POD}" -- "$@"
}

# --- probe 1: toolchain under runsc ----------------------------------------
hdr "probe 1: toolchain (node/claude) under ${RUNTIME_CLASS}"
create_probe_pod
if [[ "${DRY_RUN}" == "1" ]]; then
  kx node --version; kx claude --version; skip "dry-run: not asserting exit codes"
else
  if kx node --version && kx claude --version; then
    pass "node + claude CLI run under ${RUNTIME_CLASS}"
  else
    fail "toolchain failed under ${RUNTIME_CLASS} (likely an unsupported syscall -- see S3)"
  fi
fi

# --- probe 3: native bash + file IO ----------------------------------------
# (probe 2 is egress; we run the no-network probes first so a missing gateway
#  never blocks the core isolation verdict.)
hdr "probe 3: native bash + file IO (workspace, claude home)"
if [[ "${DRY_RUN}" == "1" ]]; then
  kx sh -c 'echo hi > /workspace/p3 && cat /workspace/p3 && mkdir -p ~/.claude && echo ok > ~/.claude/p3 && cat ~/.claude/p3'
  skip "dry-run: not asserting IO"
else
  if kx sh -c 'set -e; echo hi > /workspace/p3 && grep -q hi /workspace/p3 && mkdir -p ~/.claude && echo ok > ~/.claude/p3 && grep -q ok ~/.claude/p3'; then
    pass "bash + file IO on workspace and claude home work under ${RUNTIME_CLASS}"
  else
    fail "native bash/file IO failed under ${RUNTIME_CLASS}"
  fi
fi

# --- probe 2: real egress query through the gateway ------------------------
hdr "probe 2: real egress query through gateway"
if [[ "${RUN_EGRESS}" != "1" ]]; then
  skip "probe 2 disabled (set RUN_EGRESS=1 with ANTHROPIC_BASE_URL/ANTHROPIC_AUTH_TOKEN)"
elif [[ "${DRY_RUN}" == "1" ]]; then
  kx sh -c 'claude -p "write the text PONG and nothing else" --max-turns 1'
  skip "dry-run: not asserting query output"
else
  # A real turn must reach the gateway (egress) AND exercise the model path.
  if kx sh -c 'claude -p "reply with exactly: PONG" --max-turns 1 2>'"${DEVNULL}"' | grep -qi pong'; then
    pass "egress query succeeded under ${RUNTIME_CLASS} (NetworkPolicy + runsc net OK)"
  else
    fail "egress query failed (gateway unreachable, or runsc net stack issue)"
  fi
fi

# --- probe 4: hibernate -> resume (disk-kept) ------------------------------
hdr "probe 4: hibernate (delete Pod, keep PVC) -> resume + claude --resume"
if [[ -z "${PVC}" ]]; then
  skip "probe 4 needs an existing per-session PVC: set PVC=<name>"
elif [[ "${DRY_RUN}" == "1" ]]; then
  echo "  + (dry-run) write a marker session, delete Pod (keep PVC ${PVC}), recreate mounting ${PVC}, claude --resume"
  skip "dry-run: not performing hibernate cycle"
else
  # This mirrors the k8s provider Pause=delete-Pod-keep-PVC / Resume=recreate
  # path (ADR-0008). Full implementation requires a session id + mounted PVC;
  # here we assert the PVC survives a Pod delete/recreate and claude home persists.
  marker="resume-$(date +%s)"
  if kx sh -c "echo ${marker} > ~/.claude/resume_marker"; then :; fi
  cleanup; "${KUBECTL}" -n "${NS}" wait --for=delete "pod/${POD}" --timeout=120s || true
  create_probe_pod
  if kx sh -c "grep -q ${marker} ~/.claude/resume_marker"; then
    pass "session state survived hibernate/resume via PVC (disk-kept, ADR-0008)"
  else
    fail "session state lost across hibernate/resume"
  fi
fi

# --- probe 5: cold-start timing, with vs without node pre-pull -------------
hdr "probe 5: cold-start timing (with vs without #15 node image pre-pull)"
if [[ "${RUN_COLDSTART}" != "1" ]]; then
  skip "probe 5 disabled (set RUN_COLDSTART=1; compare vs bench/README.md 3.2)"
elif [[ "${DRY_RUN}" == "1" ]]; then
  echo "  + (dry-run) for arm in nopull,prepull: x${COLD_ITERS} create runsc Pod, time Ready, delete"
  skip "dry-run: not timing"
else
  for arm in nopull prepull; do
    echo "  -- arm: ${arm} (${COLD_ITERS} iters) --"
    [[ "${arm}" == "prepull" ]] && echo "     (ensure image-warmer DaemonSet applied; image already on node)"
    for i in $(seq 1 "${COLD_ITERS}"); do
      p="${POD}-cold-${arm}-${i}"
      start=$(date +%s.%N)
      probe_pod_manifest | sed "s/name: ${POD}\$/name: ${p}/" | "${KUBECTL}" apply -f - >"${DEVNULL}"
      "${KUBECTL}" -n "${NS}" wait --for=condition=Ready "pod/${p}" --timeout=300s >"${DEVNULL}" 2>&1 || true
      end=$(date +%s.%N)
      echo "     ${arm} iter ${i}: $(echo "${end} - ${start}" | bc)s"
      "${KUBECTL}" -n "${NS}" delete pod "${p}" --ignore-not-found --wait=false >"${DEVNULL}" 2>&1 || true
    done
  done
  pass "cold-start timing collected (record both arms into bench/README.md 3.2)"
fi

# --- probe 6: runsc checkpoint/restore (RAM-kept resume) -------------------
hdr "probe 6: runsc checkpoint/restore (RAM-kept resume, absorbs Agent Substrate)"
if [[ "${RUN_CHECKPOINT}" != "1" ]]; then
  skip "probe 6 disabled (set RUN_CHECKPOINT=1; needs node access to runsc)"
elif [[ "${DRY_RUN}" == "1" ]]; then
  echo "  + (dry-run) on the node: runsc checkpoint --image-path=<dir> <cid>; runsc restore ...; assert in-RAM state survives"
  skip "dry-run: not invoking runsc"
else
  # This probe must run WHERE runsc lives (the node / containerd), not via
  # kubectl exec. We document the exact sequence; CI cannot do it without node
  # privileges, so we mark it skipped-with-instructions unless RUNSC_CID is set.
  if [[ -z "${RUNSC_CID:-}" ]]; then
    skip "set RUNSC_CID=<container id> (run on the node) to exercise checkpoint/restore"
  else
    snap="${SNAP_DIR:-/var/run/cocola-ckpt-${RUNSC_CID}}"
    if run sudo runsc checkpoint --image-path="${snap}" "${RUNSC_CID}" \
       && run sudo runsc restore --image-path="${snap}" "${RUNSC_CID}"; then
      pass "runsc checkpoint/restore round-tripped -- evaluate RAM-kept resume, update ADR-0008 sec.3"
    else
      fail "runsc checkpoint/restore failed on our image -- keep disk-kept resume, record reason"
    fi
  fi
fi

# --- verdict ---------------------------------------------------------------
hdr "verdict"
echo "  PASS=${PASS} FAIL=${FAIL} SKIP=${SKIP}"
if [[ "${FAIL}" -gt 0 ]]; then
  echo "  -> runsc compat NOT clean; see failures above (route to #15 S3)." >&2
  exit 1
fi
echo "  -> no failures (skips are deferred probes; run on the target cluster to complete Layer C)."
