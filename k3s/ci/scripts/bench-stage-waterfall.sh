#!/usr/bin/env sh
# Extract a per-stage waterfall from a finished (or running) Jenkins build's
# console log. The timestamps plugin prefixes every line with [HH:MM:SS.mmm],
# so stage boundaries ("[Pipeline] { (Stage name)") and key inner steps give
# the breakdown; the run's root cause of pipeline latency is whichever band
# dominates. Complements bench-pipeline-timing.sh (which measures e2e).
#
# usage: bench-stage-waterfall.sh <build-number>
# Required environment: JENKINS_URL, JENKINS_AUTH
set -eu
JENKINS_URL=${JENKINS_URL:?JENKINS_URL must be set}
JENKINS_AUTH=${JENKINS_AUTH:?JENKINS_AUTH must be set}
[ $# -eq 1 ] || { echo "usage: $0 <build-number>" >&2; exit 2; }

repo_root=$(git rev-parse --show-toplevel)
results_dir="${repo_root}/benchmarks/results"
mkdir -p "$results_dir"

curl -fsS -u "$JENKINS_AUTH" "$JENKINS_URL/job/ecampus-pipeline/$1/consoleText" > /tmp/waterfall-console.$$ 2>/dev/null || {
  echo "cannot fetch console for build $1" >&2; exit 1; }

awk '''
  {
    line = $0
    h = index(line, "[Pipeline] { (")
    if (h > 0) {
      rest = substr(line, h + 14)
      name = substr(rest, 1, index(rest, ")") - 1)
      if (name != "Branch:" && substr(name, 1, 7) != "Branch:") {
        cur = name
        if (!(cur in first)) order[++n] = cur
      }
      next
    }
    if (cur == "" || (cur in first)) next
    if (substr(line, 1, 1) != "[") next
    t = index(line, "T")
    if (t == 0) next
    ts = substr(line, t + 1, 8)
    split(ts, p, ":"); first[cur] = p[1]*3600 + p[2]*60 + p[3]
  }
  END {
    for (i = 1; i <= n; i++) {
      if (!(order[i] in first)) { printf "%s\tskipped\n", order[i]; continue }
      d = 0
      j = i + 1
      while (j <= n && !(order[j] in first)) j++
      if (j <= n && first[order[j]] >= first[order[i]]) d = first[order[j]] - first[order[i]]
      printf "%s\t%ds\n", order[i], d
    }
  }
' /tmp/waterfall-console.$$ | tee "${results_dir}/waterfall-$1.tsv"
rm -f /tmp/waterfall-console.$$

echo
echo "dominant stages:"
sort -t$'\t' -k2 -rn "${results_dir}/waterfall-$1.tsv" | head -5
