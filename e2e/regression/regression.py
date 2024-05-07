from prometheus_api_client import PrometheusConnect, PrometheusApiClientException
import logging
import sys

logging.basicConfig(
    level=logging.INFO, format="%(asctime)s - %(levelname)s - %(message)s"
)


def connect_prometheus(server_url):
    """
    Connect to prometheus server
    """
    try:
        prom = PrometheusConnect(url=server_url, disable_ssl=True)
        test_query = prom.custom_query(query="up")
        if not test_query:
            logging.error(
                "Failed to retrieve data from Prometheus, the server may be down or not responding."
            )
            sys.exit(1)
        logging.info("connection to prometheus server successfully")
        return prom
    except PrometheusApiClientException as e:
        logging.error(f"Prometheus API client exception occured: {e}")
        sys.exit(1)
    except Exception as e:
        logging.error(f"failed to connect to prometheus server: {e}")
        sys.exit(1)


def get_metric_value(prom, metric_name):
    """
    Retrive the current value of a metric from prometheus
    """
    try:
        result = prom.get_current_metric_value(metric_name=metric_name)
        logging.info(f"data retrieved for metric {metric_name}: {result}")
        return extract_value(result)
    except PrometheusApiClientException as e:
        logging.error(f"failed to reterive data for {metric_name}: {e}")
        sys.exit(1)
    except Exception as e:
        logging.error(f"unexcepted error while reteriving {metric_name}: {e}")
        sys.exit(1)


def extract_value(result):
    """
    Extract the value from prometheus query result
    """
    if result and "value" in result[0]:
        try:
            return float(result[0]["value"][1])
        except (ValueError, TypeError) as e:
            logging.error(f"error converting result to float: {e}")
    return None


def compute(dev_val, latest_val, threshold):
    """
    Compute the ratio of two values and comparing against threshold
    """
    try:
        if dev_val is not None and latest_val is not None and latest_val != 0:
            ratio = dev_val / latest_val
            ratio = round(ratio, 1)
            percentage_change = (ratio - 1.0) * 100
            logging.info(f"ratio of dev to latest version: {ratio:.2f}")
            logging.info(f"percentage change: {percentage_change:2f}%")

            if ratio > 0 and ratio <= threshold:
                status = "satisfactory"
            else:
                status = "unsatisfactory"
            return status
        else:
            logging.warning(
                "invalid data for ratio computation: values are None or zero"
            )
            return "data error"
    except Exception as e:
        logging.error(f"error computing ratio: {e}")
        return "error"


def main():
    prometheus_url = "http://localhost:9090"
    metrics = [
        (
            'sum(rate(kepler_node_platform_joules_total{job="dev"}[5m]))',
            'sum(rate(kepler_node_platform_joules_total{job="latest"}[5m]))',
        ),
        (
            'sum(rate(kepler_node_core_joules_total{job="dev"}[5m]))',
            'sum(rate(kepler_node_core_joules_total{job="latest"}[5m]))',
        ),
        (
            'sum(rate(kepler_node_dram_joules_total{job="dev"}[5m]))',
            'sum(rate(kepler_node_dram_joules_total{job="latest"}[5m]))',
        ),
        (
            'sum(rate(kepler_node_package_joules_total{job="dev"}[5m]))',
            'sum(rate(kepler_node_package_joules_total{job="latest"}[5m]))',
        ),
        (
            'sum(rate(kepler_node_uncore_joules_total{job="dev"}[5m]))',
            'sum(rate(kepler_node_uncore_joules_total{job="latest"}[5m]))',
        ),
        (
            'sum((kepler_container_bpf_block_irq_total{job="dev"}))',
            'sum((kepler_container_bpf_block_irq_total{job="latest"}))',
        ),
        (
            'sum((kepler_container_bpf_cpu_time_ms_total{job="dev"}))',
            'sum((kepler_container_bpf_cpu_time_ms_total{job="latest"}))',
        ),
        (
            'sum((kepler_container_bpf_net_rx_irq_total{job="dev"}))',
            'sum((kepler_container_bpf_net_rx_irq_total{job="latest"}))',
        ),
    ]

    threshold = 2.0

    prom = connect_prometheus(server_url=prometheus_url)
    if not prom:
        return

    for dev, latest in metrics:
        dev_val = get_metric_value(prom, metric_name=dev)
        latest_val = get_metric_value(prom, metric_name=latest)

        status = compute(dev_val, latest_val, threshold)

        if status == "unsatisfactory":
            logging.error("Unsatisfactory ratio computed")
            sys.exit(1)


if __name__ == "__main__":
    main()
