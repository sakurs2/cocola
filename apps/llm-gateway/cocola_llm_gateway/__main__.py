"""M0 entrypoint — banner only."""
from cocola_common import get_logger


def main() -> None:
    log = get_logger("cocola.llm-gateway")
    log.info("cocola-llm-gateway started", milestone="M0")


if __name__ == "__main__":
    main()
