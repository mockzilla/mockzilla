#!/usr/bin/env bash
# Run TestPortableIntegration. With no SPEC/SPECS, scans testdata/specs and
# runs all specs in batches.
#
# Env:
#   SPEC=path            run one spec
#   SPECS="a b c"        run a whitespace-separated list
#   MAX_CONCURRENCY=N    go test -parallel (default: GOMAXPROCS single, 4 batched)
#   TEST_TIMEOUT=30m     go test -timeout
#   BATCH_SIZE=100       specs per batch
#   NO_CACHE=1           bypass pass-cache
#   CLEAR_CACHE=1        wipe pass-cache before running

set -euo pipefail

# stats_log accumulates the per-spec result lines (both ok and FAIL)
# emitted by portable_integration_test.printSpecLine. Each go-test
# invocation prints its own summary at exit, but across many batches
# there's no aggregate — this file is what the end-of-run aggregator
# reads to roll the numbers up.
stats_log=$(mktemp -t portable-int-stats.XXXXXX)
trap 'rm -f "$stats_log"' EXIT

run_batch() {
    local specs="$1" parallel=() rc=0
    [[ -n "${MAX_CONCURRENCY:-}" ]] && parallel=(-parallel="$MAX_CONCURRENCY")
    # tee through awk: pass every line through to the operator, and
    # split off the "[N/M] (ok|FAIL) <spec> (...)" lines into stats_log
    # so the cross-batch aggregator can roll them up at the very end.
    set +e
    SPEC="${SPEC:-}" SPECS="$specs" \
        go test -v -run='^TestPortableIntegration$' ${parallel[@]+"${parallel[@]}"} \
            -timeout="${TEST_TIMEOUT:-30m}" -count=1 . 2>&1 \
        | grep -vE '^(=== (RUN|PAUSE|CONT|NAME)|    --- (PASS|FAIL):)' \
        | awk -v outfile="$stats_log" '
            /^[[:space:]]*\[[0-9]+\/[0-9]+\] (ok|FAIL) / { print >> outfile }
            { print }
        '
    rc=${PIPESTATUS[0]}
    set -e
    return "$rc"
}

if [[ -n "${SPEC:-}" || -n "${SPECS:-}" ]]; then
    run_batch "${SPECS:-}"
    exit
fi

all=()
while IFS= read -r line; do all+=("$line"); done < <(find testdata/specs -type f \
    \( -name '*.yml' -o -name '*.yaml' -o -name '*.json' \) \
    ! -name '-*' -not -path '*/stash/*' | sort)
total_all=${#all[@]}

cache=.portable-integration-cache.json
[[ -n "${CLEAR_CACHE:-}" ]] && rm -f "$cache"

if [[ -f $cache && -z "${NO_CACHE:-}" ]]; then
    specs=()
    while IFS= read -r line; do specs+=("$line"); done < <(comm -23 \
        <(printf '%s\n' "${all[@]}") \
        <(python3 -c "
import json, os
d = json.load(open('$cache'))
cwd = os.getcwd() + '/'
for p, e in d.get('entries', {}).items():
    if e.get('passed'):
        print(p[len(cwd):] if p.startswith(cwd) else p)
" | sort))
else
    specs=("${all[@]}")
fi

total=${#specs[@]}
if [[ $total -eq 0 ]]; then
    echo "=== All $total_all specs cached as passing (use CLEAR_CACHE=1 to retest) ==="
    exit 0
fi

cached_n=$((total_all - total))
batch_size=${BATCH_SIZE:-100}
total_batches=$(( (total + batch_size - 1) / batch_size ))

if [[ $cached_n -gt 0 ]]; then
    echo "=== $total uncached ($cached_n cached, $total_all total) in $total_batches batches of $batch_size ==="
else
    echo "=== $total specs in $total_batches batches of $batch_size ==="
fi

: "${MAX_CONCURRENCY:=4}"

batch=1
i=0
failed_batches=0
while (( i < total )); do
    end=$(( i + batch_size ))
    (( end > total )) && end=$total
    echo
    echo "=== Batch $batch/$total_batches: specs $((i+1))-$end ==="
    # Don't bail on a single failing batch — keep running so the
    # operator gets a full picture of which specs failed, not just
    # the first batch that tripped.
    run_batch "$(printf '%s\n' "${specs[@]:i:$((end-i))}")" || failed_batches=$((failed_batches + 1))
    i=$end
    batch=$((batch + 1))
done

echo
# Cross-batch aggregate. Mirrors the codegen suite's ReportResults
# layout (totals + failed specs list) but rolls across every go-test
# invocation rather than printing per-batch summaries only.
if [[ -s "$stats_log" ]]; then
    python3 - "$stats_log" "$failed_batches" "$total_batches" <<'PY'
import re, sys
stats_path, failed_batches, total_batches = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])

# Line shapes from portable_integration_test.printSpecLine:
#   [N/M] ok   <spec> (boot=Xms test=Yms, R routes, F failures)
#   [N/M] FAIL <spec> (boot=Xms test=Yms, R routes, F failures)
#   [N/M] FAIL <spec> (boot error: <msg>)
rx = re.compile(
    r'^\s*\[\d+/\d+\] (ok|FAIL) (.+?) \('
    r'(?:boot=([\d.]+)(ms|s) test=([\d.]+)(ms|s), (\d+) routes, (\d+) failures'
    r'|boot error: .+)'
    r'\)\s*$'
)

def ms(value, unit):
    return float(value) * (1000.0 if unit == 's' else 1.0)

def fmt(ms_val):
    s = ms_val / 1000.0
    if s < 60:
        return f"{s:.1f}s"
    m, s = divmod(s, 60)
    return f"{int(m)}m{s:.0f}s"

specs_ok = specs_fail = routes = failures = 0
boot_ms = test_ms = 0.0
failed_specs = []  # preserve first-seen order
seen_failed = set()

with open(stats_path) as f:
    for line in f:
        m = rx.match(line)
        if not m:
            continue
        status, spec = m.group(1), m.group(2)
        if status == 'ok':
            specs_ok += 1
        else:
            specs_fail += 1
            if spec not in seen_failed:
                seen_failed.add(spec)
                failed_specs.append(spec)
        if m.group(3):
            boot_ms += ms(m.group(3), m.group(4))
            test_ms += ms(m.group(5), m.group(6))
            routes += int(m.group(7))
            failures += int(m.group(8))

total_specs = specs_ok + specs_fail
print()
print("=== Portable Integration Results (all batches) ===")
print(f"Total specs:        {total_specs} (ok: {specs_ok}, failed: {specs_fail})")
print(f"Total operations:   {routes} (failures: {failures})")
print(f"Boot time:          {fmt(boot_ms)}")
print(f"Test time:          {fmt(test_ms)}")
print(f"Total time:         {fmt(boot_ms + test_ms)}")
if total_specs > 0:
    print(f"Avg per spec:       {fmt((boot_ms + test_ms) / total_specs)}")
print(f"Batches:            {total_batches} (failing: {failed_batches})")
print("=" * 50)

if failed_specs:
    print()
    print(f"=== Failed specs ({len(failed_specs)}) ===")
    for s in sorted(failed_specs):
        print(f"  {s}")
    sys.exit(1)
PY
    rc=$?
    (( rc != 0 )) && exit "$rc"
fi
echo "=== Done ==="
