#!/usr/bin/env bash
# Run TestPortableIntegration. With no SPEC/SPECS, scans testdata/specs and
# runs all specs in batches (libopenapi leaks across many specs and OOMs
# in a single go test invocation).
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

run_batch() {
    local specs="$1" parallel=()
    [[ -n "${MAX_CONCURRENCY:-}" ]] && parallel=(-parallel="$MAX_CONCURRENCY")
    SPEC="${SPEC:-}" SPECS="$specs" \
        go test -v -run='^TestPortableIntegration$' "${parallel[@]}" \
            -timeout="${TEST_TIMEOUT:-30m}" -count=1 . 2>&1 \
        | grep -vE '^(=== (RUN|PAUSE|CONT|NAME)|    --- (PASS|FAIL):)'
}

if [[ -n "${SPEC:-}" || -n "${SPECS:-}" ]]; then
    run_batch "${SPECS:-}"
    exit
fi

mapfile -t all < <(find testdata/specs -type f \
    \( -name '*.yml' -o -name '*.yaml' -o -name '*.json' \) \
    ! -name '-*' -not -path '*/stash/*' | sort)
total_all=${#all[@]}

cache=.portable-integration-cache.json
[[ -n "${CLEAR_CACHE:-}" ]] && rm -f "$cache"

if [[ -f $cache && -z "${NO_CACHE:-}" ]]; then
    mapfile -t specs < <(comm -23 \
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
while (( i < total )); do
    end=$(( i + batch_size ))
    (( end > total )) && end=$total
    echo
    echo "=== Batch $batch/$total_batches: specs $((i+1))-$end ==="
    run_batch "${specs[*]:i:$((end-i))}" || true
    i=$end
    batch=$((batch + 1))
done

echo
echo "=== Done ==="
