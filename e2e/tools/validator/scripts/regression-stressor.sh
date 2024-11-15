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

generate_parabola_load_curve() {
	local total_time=$1
	local step_time=$2
	local max_load=$3
	local -n load_curve_ref=$4 # Requires Bash 4.3 or later

	local steps=$((total_time / step_time))
	for ((i = 0; i <= steps; i++)); do
		local t=$((i * step_time))
		# Parabolic function to calculate load
		local load=$(echo "((-4 * $max_load) * ($t - $total_time / 2)^2) / ($total_time^2) + $max_load + 0.5" | bc -l | cut -d. -f1)
		# Ensure load is not negative or exceeds max_load
		if [ -z "$load" ] || [ "$load" -lt 0 ]; then
			load=0
		elif [ "$load" -gt "$max_load" ]; then
			load="$max_load"
		fi
		# Append to load_curve array
		load_curve_ref+=("$load:$step_time")
	done
}

main() {
	local cpus
	cpus=$(nproc)

	# Initialize load_curve array
	local -a load_curve=()

	# Generate 3 parabolic load curves with max_load of 80%
	local num_parabolas=3
	local total_time=60 # Duration of one parabola in seconds
	local step_time=5   # Time interval between load steps
	local max_load=75   # Maximum CPU load percentage

	for ((p = 1; p <= num_parabolas; p++)); do
		generate_parabola_load_curve "$total_time" "$step_time" "$max_load" load_curve
		# Optional pause between parabolas
		load_curve+=("0:10") # 0% load for 10 seconds between parabolas
	done

	# Warmup phase
	echo "Warmup ..."
	run stress-ng --cpu "$cpus" --cpu-method ackermann --cpu-load 0 --timeout 5

	# Execute the load curve
	for x in "${load_curve[@]}"; do
		local load="${x%%:*}"
		local time="${x##*:}s"
		run stress-ng --cpu "$cpus" --cpu-method ackermann --cpu-load "$load" --timeout "$time"
	done
	# Cleanup phase
	echo "Cleanup ..."
	run stress-ng --cpu "$cpus" --cpu-method ackermann --cpu-load 0 --timeout 20
}

main "$@"
