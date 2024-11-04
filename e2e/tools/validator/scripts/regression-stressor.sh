#!/usr/bin/env bash

set -eu -o pipefail

trap exit_all INT
exit_all() {
	pkill -P $$
}

run() {
	echo "❯ $*"
	"$@"
	echo "      ‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾"
}

main() {

	local cpus
	cpus=$(nproc)

	# load and time
	local -a load_curve=(
		0:15
		10:30
		25:30
		50:30
		75:30
		50:30
		25:30
		10:30
		0:15
	)
	# sleep 5  so that first run and the second run look the same
	echo "Warmup .."
	run stress-ng --cpu "$cpus" --cpu-method ackermann --cpu-load 0 --timeout 15

	for x in "${load_curve[@]}"; do
		local load="${x%%:*}"
		local time="${x##*:}s"
		run stress-ng --cpu "$cpus" --cpu-method ackermann --cpu-load "$load" --timeout "$time"
	done
	# sleep 5  so that first run and the second run look the same
	echo "Cooldown .."
	run stress-ng --cpu "$cpus" --cpu-method ackermann --cpu-load 0 --timeout 60
}

main "$@"
