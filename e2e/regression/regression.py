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
        logging.info("connection to prometheus server successfully")
        return prom
    except Exception as e:
        logging.error(f"failed to connect to prometheus server: {e}")
        return None


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
        return None
    except Exception as e:
        logging.error(f"unexcepted error while reteriving {metric_name}: {e}")
        return None


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


def compute_ratio(first_val, second_val, threshold):
    """
    Compute the ratio of two values and comparing against threshold
    """
    try:
        if first_val is not None and second_val is not None and second_val != 0:
            ratio = first_val / second_val
            ratio = round(ratio, 1)
            status = "satisfactory" if ratio <= threshold else "unsatisfactory"
            return ratio, status
        else:
            logging.warning(
                "invalid data for ratio computation: values are None or zero"
            )
            return None, "data error"
    except Exception as e:
        logging.error(f"error computing ratio: {e}")
        return None, "error"


def main():
    prometheus_url = "http://localhost:9090"
    metrics = [
        (
            'sum(rate(kepler_node_platform_joules_total{job="latest"}[1m]))',
            'sum(rate(kepler_node_platform_joules_total{job="dev"}[1m]))',
        ),
        (
            'sum(rate(kepler_node_core_joules_total{job="latest"}[1m]))',
            'sum(rate(kepler_node_core_joules_total{job="dev"}[1m]))',
        ),
        (
            'sum(rate(kepler_node_dram_joules_total{job="latest"}[1m]))',
            'sum(rate(kepler_node_dram_joules_total{job="dev"}[1m]))',
        ),
        (
            'sum(rate(kepler_node_package_joules_total{job="latest"}[1m]))',
            'sum(rate(kepler_node_package_joules_total{job="dev"}[1m]))',
        ),
        (
            'sum(rate(kepler_node_uncore_joules_total{job="latest"}[1m]))',
            'sum(rate(kepler_node_uncore_joules_total{job="dev"}[1m]))',
        ),
    ]

    threshold = 1.9

    prom = connect_prometheus(server_url=prometheus_url)
    if not prom:
        return

    for latest, dev in metrics:
        latest_platform_val = get_metric_value(prom, metric_name=latest)
        dev_platform_val = get_metric_value(prom, metric_name=dev)

        ratio, status = compute_ratio(latest_platform_val, dev_platform_val, threshold)
        if status == "unsatisfactory":
            logging.error(
                f"Unsatisfactory ratio computed: {ratio} is below the threshold {threshold}"
            )
            sys.exit(1)

        if ratio is not None:
            logging.info(f"ratio of {latest} to {dev}: {ratio}")
        else:
            logging.info("ratio could not be calculated due to missing or invalid data")


if __name__ == "__main__":
    main()
