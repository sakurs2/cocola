from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ExecEventKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    EXEC_EVENT_KIND_UNSPECIFIED: _ClassVar[ExecEventKind]
    EXEC_EVENT_KIND_STDOUT: _ClassVar[ExecEventKind]
    EXEC_EVENT_KIND_STDERR: _ClassVar[ExecEventKind]
    EXEC_EVENT_KIND_EXIT: _ClassVar[ExecEventKind]
    EXEC_EVENT_KIND_ERROR: _ClassVar[ExecEventKind]
EXEC_EVENT_KIND_UNSPECIFIED: ExecEventKind
EXEC_EVENT_KIND_STDOUT: ExecEventKind
EXEC_EVENT_KIND_STDERR: ExecEventKind
EXEC_EVENT_KIND_EXIT: ExecEventKind
EXEC_EVENT_KIND_ERROR: ExecEventKind

class Resources(_message.Message):
    __slots__ = ("cpu_cores", "memory_mib", "disk_mib")
    CPU_CORES_FIELD_NUMBER: _ClassVar[int]
    MEMORY_MIB_FIELD_NUMBER: _ClassVar[int]
    DISK_MIB_FIELD_NUMBER: _ClassVar[int]
    cpu_cores: float
    memory_mib: int
    disk_mib: int
    def __init__(self, cpu_cores: _Optional[float] = ..., memory_mib: _Optional[int] = ..., disk_mib: _Optional[int] = ...) -> None: ...

class SandboxSpec(_message.Message):
    __slots__ = ("user_id", "session_id", "image", "env", "resources", "egress_allowlist")
    class EnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    RESOURCES_FIELD_NUMBER: _ClassVar[int]
    EGRESS_ALLOWLIST_FIELD_NUMBER: _ClassVar[int]
    user_id: str
    session_id: str
    image: str
    env: _containers.ScalarMap[str, str]
    resources: Resources
    egress_allowlist: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., image: _Optional[str] = ..., env: _Optional[_Mapping[str, str]] = ..., resources: _Optional[_Union[Resources, _Mapping]] = ..., egress_allowlist: _Optional[_Iterable[str]] = ...) -> None: ...

class Sandbox(_message.Message):
    __slots__ = ("id", "user_id", "session_id", "endpoint")
    ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    id: str
    user_id: str
    session_id: str
    endpoint: str
    def __init__(self, id: _Optional[str] = ..., user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., endpoint: _Optional[str] = ...) -> None: ...

class CreateRequest(_message.Message):
    __slots__ = ("spec",)
    SPEC_FIELD_NUMBER: _ClassVar[int]
    spec: SandboxSpec
    def __init__(self, spec: _Optional[_Union[SandboxSpec, _Mapping]] = ...) -> None: ...

class CreateResponse(_message.Message):
    __slots__ = ("sandbox",)
    SANDBOX_FIELD_NUMBER: _ClassVar[int]
    sandbox: Sandbox
    def __init__(self, sandbox: _Optional[_Union[Sandbox, _Mapping]] = ...) -> None: ...

class ExecRequest(_message.Message):
    __slots__ = ("sandbox_id", "cmd", "cwd", "env", "stdin", "timeout_secs")
    class EnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    CMD_FIELD_NUMBER: _ClassVar[int]
    CWD_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    STDIN_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_SECS_FIELD_NUMBER: _ClassVar[int]
    sandbox_id: str
    cmd: _containers.RepeatedScalarFieldContainer[str]
    cwd: str
    env: _containers.ScalarMap[str, str]
    stdin: bytes
    timeout_secs: int
    def __init__(self, sandbox_id: _Optional[str] = ..., cmd: _Optional[_Iterable[str]] = ..., cwd: _Optional[str] = ..., env: _Optional[_Mapping[str, str]] = ..., stdin: _Optional[bytes] = ..., timeout_secs: _Optional[int] = ...) -> None: ...

class ExecEvent(_message.Message):
    __slots__ = ("kind", "stdout", "stderr", "exit_code", "error")
    KIND_FIELD_NUMBER: _ClassVar[int]
    STDOUT_FIELD_NUMBER: _ClassVar[int]
    STDERR_FIELD_NUMBER: _ClassVar[int]
    EXIT_CODE_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    kind: ExecEventKind
    stdout: bytes
    stderr: bytes
    exit_code: int
    error: str
    def __init__(self, kind: _Optional[_Union[ExecEventKind, str]] = ..., stdout: _Optional[bytes] = ..., stderr: _Optional[bytes] = ..., exit_code: _Optional[int] = ..., error: _Optional[str] = ...) -> None: ...

class WriteFileRequest(_message.Message):
    __slots__ = ("sandbox_id", "path", "data")
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    sandbox_id: str
    path: str
    data: bytes
    def __init__(self, sandbox_id: _Optional[str] = ..., path: _Optional[str] = ..., data: _Optional[bytes] = ...) -> None: ...

class WriteFileResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ReadFileRequest(_message.Message):
    __slots__ = ("sandbox_id", "path")
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    sandbox_id: str
    path: str
    def __init__(self, sandbox_id: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class ReadFileResponse(_message.Message):
    __slots__ = ("data",)
    DATA_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    def __init__(self, data: _Optional[bytes] = ...) -> None: ...

class PauseRequest(_message.Message):
    __slots__ = ("sandbox_id",)
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    sandbox_id: str
    def __init__(self, sandbox_id: _Optional[str] = ...) -> None: ...

class PauseResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ResumeRequest(_message.Message):
    __slots__ = ("sandbox_id",)
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    sandbox_id: str
    def __init__(self, sandbox_id: _Optional[str] = ...) -> None: ...

class ResumeResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class DestroyRequest(_message.Message):
    __slots__ = ("sandbox_id",)
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    sandbox_id: str
    def __init__(self, sandbox_id: _Optional[str] = ...) -> None: ...

class DestroyResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class HealthRequest(_message.Message):
    __slots__ = ("sandbox_id",)
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    sandbox_id: str
    def __init__(self, sandbox_id: _Optional[str] = ...) -> None: ...

class HealthResponse(_message.Message):
    __slots__ = ("healthy", "detail")
    HEALTHY_FIELD_NUMBER: _ClassVar[int]
    DETAIL_FIELD_NUMBER: _ClassVar[int]
    healthy: bool
    detail: str
    def __init__(self, healthy: bool = ..., detail: _Optional[str] = ...) -> None: ...

class AcquireRequest(_message.Message):
    __slots__ = ("session_id", "user_id", "image", "env")
    class EnvEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    IMAGE_FIELD_NUMBER: _ClassVar[int]
    ENV_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    user_id: str
    image: str
    env: _containers.ScalarMap[str, str]
    def __init__(self, session_id: _Optional[str] = ..., user_id: _Optional[str] = ..., image: _Optional[str] = ..., env: _Optional[_Mapping[str, str]] = ...) -> None: ...

class AcquireResponse(_message.Message):
    __slots__ = ("sandbox", "reused")
    SANDBOX_FIELD_NUMBER: _ClassVar[int]
    REUSED_FIELD_NUMBER: _ClassVar[int]
    sandbox: Sandbox
    reused: bool
    def __init__(self, sandbox: _Optional[_Union[Sandbox, _Mapping]] = ..., reused: bool = ...) -> None: ...

class HeartbeatRequest(_message.Message):
    __slots__ = ("sandbox_id",)
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    sandbox_id: str
    def __init__(self, sandbox_id: _Optional[str] = ...) -> None: ...

class HeartbeatResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ReleaseRequest(_message.Message):
    __slots__ = ("session_id",)
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    session_id: str
    def __init__(self, session_id: _Optional[str] = ...) -> None: ...

class ReleaseResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
