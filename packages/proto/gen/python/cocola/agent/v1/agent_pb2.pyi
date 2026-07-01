from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class QueryRequest(_message.Message):
    __slots__ = ("user_id", "session_id", "prompt", "sandbox_id", "max_turns", "attachments")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    MAX_TURNS_FIELD_NUMBER: _ClassVar[int]
    ATTACHMENTS_FIELD_NUMBER: _ClassVar[int]
    user_id: str
    session_id: str
    prompt: str
    sandbox_id: str
    max_turns: int
    attachments: _containers.RepeatedCompositeFieldContainer[Attachment]
    def __init__(self, user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., prompt: _Optional[str] = ..., sandbox_id: _Optional[str] = ..., max_turns: _Optional[int] = ..., attachments: _Optional[_Iterable[_Union[Attachment, _Mapping]]] = ...) -> None: ...

class Attachment(_message.Message):
    __slots__ = ("filename", "content", "mime", "oss_key", "size")
    FILENAME_FIELD_NUMBER: _ClassVar[int]
    CONTENT_FIELD_NUMBER: _ClassVar[int]
    MIME_FIELD_NUMBER: _ClassVar[int]
    OSS_KEY_FIELD_NUMBER: _ClassVar[int]
    SIZE_FIELD_NUMBER: _ClassVar[int]
    filename: str
    content: bytes
    mime: str
    oss_key: str
    size: int
    def __init__(self, filename: _Optional[str] = ..., content: _Optional[bytes] = ..., mime: _Optional[str] = ..., oss_key: _Optional[str] = ..., size: _Optional[int] = ...) -> None: ...

class AgentEvent(_message.Message):
    __slots__ = ("kind", "data")
    class DataEntry(_message.Message):
        __slots__ = ("key", "value")
        KEY_FIELD_NUMBER: _ClassVar[int]
        VALUE_FIELD_NUMBER: _ClassVar[int]
        key: str
        value: str
        def __init__(self, key: _Optional[str] = ..., value: _Optional[str] = ...) -> None: ...
    KIND_FIELD_NUMBER: _ClassVar[int]
    DATA_FIELD_NUMBER: _ClassVar[int]
    kind: str
    data: _containers.ScalarMap[str, str]
    def __init__(self, kind: _Optional[str] = ..., data: _Optional[_Mapping[str, str]] = ...) -> None: ...
