import base64
import hashlib
import os

import pytest
from cocola_common import CocolaError, ErrorCode
from cocola_llm_gateway.db_registry import decrypt_secret
from cryptography.hazmat.primitives.ciphers.aead import AESGCM


def _ciphertext(secret: str, plaintext: str) -> str:
    key = hashlib.sha256(secret.encode("utf-8")).digest()
    nonce = os.urandom(12)
    sealed = AESGCM(key).encrypt(nonce, plaintext.encode("utf-8"), None)
    return "v1:" + base64.b64encode(nonce + sealed).decode("ascii")


def test_decrypt_secret_round_trips_v1_ciphertext():
    ciphertext = _ciphertext("model-secret", "sk-test-key")
    assert decrypt_secret("model-secret", ciphertext) == "sk-test-key"


def test_decrypt_secret_requires_model_secret_key():
    with pytest.raises(CocolaError) as ei:
        decrypt_secret("", _ciphertext("model-secret", "sk-test-key"))
    assert ei.value.code is ErrorCode.INVALID_ARGUMENT
