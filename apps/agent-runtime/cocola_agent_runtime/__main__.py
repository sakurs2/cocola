"""M0 entrypoint — only prints a banner. Real gRPC server lands in M2."""
from cocola_common import get_logger


def main() -> None:
    log = get_logger("cocola.agent-runtime")
    log.info("cocola-agent-runtime started", milestone="M0")


if __name__ == "__main__":
    main()
