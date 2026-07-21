from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class QueryRequest(_message.Message):
    __slots__ = ("user_id", "session_id", "prompt", "sandbox_id", "max_turns", "attachments", "runtime_id", "skill_id", "allow_workspace_reset", "memory_context", "project_context")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PROMPT_FIELD_NUMBER: _ClassVar[int]
    SANDBOX_ID_FIELD_NUMBER: _ClassVar[int]
    MAX_TURNS_FIELD_NUMBER: _ClassVar[int]
    ATTACHMENTS_FIELD_NUMBER: _ClassVar[int]
    RUNTIME_ID_FIELD_NUMBER: _ClassVar[int]
    SKILL_ID_FIELD_NUMBER: _ClassVar[int]
    ALLOW_WORKSPACE_RESET_FIELD_NUMBER: _ClassVar[int]
    MEMORY_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    PROJECT_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    user_id: str
    session_id: str
    prompt: str
    sandbox_id: str
    max_turns: int
    attachments: _containers.RepeatedCompositeFieldContainer[Attachment]
    runtime_id: str
    skill_id: str
    allow_workspace_reset: bool
    memory_context: str
    project_context: ProjectContext
    def __init__(self, user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., prompt: _Optional[str] = ..., sandbox_id: _Optional[str] = ..., max_turns: _Optional[int] = ..., attachments: _Optional[_Iterable[_Union[Attachment, _Mapping]]] = ..., runtime_id: _Optional[str] = ..., skill_id: _Optional[str] = ..., allow_workspace_reset: bool = ..., memory_context: _Optional[str] = ..., project_context: _Optional[_Union[ProjectContext, _Mapping]] = ...) -> None: ...

class ProjectContext(_message.Message):
    __slots__ = ("project_id", "repository_id", "clone_url", "default_branch", "base_sha", "task_branch", "git_author_name", "git_author_email")
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    REPOSITORY_ID_FIELD_NUMBER: _ClassVar[int]
    CLONE_URL_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_BRANCH_FIELD_NUMBER: _ClassVar[int]
    BASE_SHA_FIELD_NUMBER: _ClassVar[int]
    TASK_BRANCH_FIELD_NUMBER: _ClassVar[int]
    GIT_AUTHOR_NAME_FIELD_NUMBER: _ClassVar[int]
    GIT_AUTHOR_EMAIL_FIELD_NUMBER: _ClassVar[int]
    project_id: str
    repository_id: int
    clone_url: str
    default_branch: str
    base_sha: str
    task_branch: str
    git_author_name: str
    git_author_email: str
    def __init__(self, project_id: _Optional[str] = ..., repository_id: _Optional[int] = ..., clone_url: _Optional[str] = ..., default_branch: _Optional[str] = ..., base_sha: _Optional[str] = ..., task_branch: _Optional[str] = ..., git_author_name: _Optional[str] = ..., git_author_email: _Optional[str] = ...) -> None: ...

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

class ReleaseSessionRequest(_message.Message):
    __slots__ = ("user_id", "session_id")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    user_id: str
    session_id: str
    def __init__(self, user_id: _Optional[str] = ..., session_id: _Optional[str] = ...) -> None: ...

class ReleaseSessionResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ListRuntimesRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class Runtime(_message.Message):
    __slots__ = ("id", "label", "model_protocol", "is_default")
    ID_FIELD_NUMBER: _ClassVar[int]
    LABEL_FIELD_NUMBER: _ClassVar[int]
    MODEL_PROTOCOL_FIELD_NUMBER: _ClassVar[int]
    IS_DEFAULT_FIELD_NUMBER: _ClassVar[int]
    id: str
    label: str
    model_protocol: str
    is_default: bool
    def __init__(self, id: _Optional[str] = ..., label: _Optional[str] = ..., model_protocol: _Optional[str] = ..., is_default: bool = ...) -> None: ...

class ListRuntimesResponse(_message.Message):
    __slots__ = ("runtimes",)
    RUNTIMES_FIELD_NUMBER: _ClassVar[int]
    runtimes: _containers.RepeatedCompositeFieldContainer[Runtime]
    def __init__(self, runtimes: _Optional[_Iterable[_Union[Runtime, _Mapping]]] = ...) -> None: ...

class InspectWorkspaceGitRequest(_message.Message):
    __slots__ = ("user_id", "session_id", "operation", "path", "diff_target", "project_context")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    DIFF_TARGET_FIELD_NUMBER: _ClassVar[int]
    PROJECT_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    user_id: str
    session_id: str
    operation: str
    path: str
    diff_target: str
    project_context: ProjectContext
    def __init__(self, user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., operation: _Optional[str] = ..., path: _Optional[str] = ..., diff_target: _Optional[str] = ..., project_context: _Optional[_Union[ProjectContext, _Mapping]] = ...) -> None: ...

class GitChange(_message.Message):
    __slots__ = ("path", "old_path", "status", "area")
    PATH_FIELD_NUMBER: _ClassVar[int]
    OLD_PATH_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    AREA_FIELD_NUMBER: _ClassVar[int]
    path: str
    old_path: str
    status: str
    area: str
    def __init__(self, path: _Optional[str] = ..., old_path: _Optional[str] = ..., status: _Optional[str] = ..., area: _Optional[str] = ...) -> None: ...

class GitSnapshot(_message.Message):
    __slots__ = ("branch", "base_sha", "head_sha", "ahead", "dirty", "changes", "truncated", "base_ref")
    BRANCH_FIELD_NUMBER: _ClassVar[int]
    BASE_SHA_FIELD_NUMBER: _ClassVar[int]
    HEAD_SHA_FIELD_NUMBER: _ClassVar[int]
    AHEAD_FIELD_NUMBER: _ClassVar[int]
    DIRTY_FIELD_NUMBER: _ClassVar[int]
    CHANGES_FIELD_NUMBER: _ClassVar[int]
    TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    BASE_REF_FIELD_NUMBER: _ClassVar[int]
    branch: str
    base_sha: str
    head_sha: str
    ahead: int
    dirty: bool
    changes: _containers.RepeatedCompositeFieldContainer[GitChange]
    truncated: bool
    base_ref: str
    def __init__(self, branch: _Optional[str] = ..., base_sha: _Optional[str] = ..., head_sha: _Optional[str] = ..., ahead: _Optional[int] = ..., dirty: bool = ..., changes: _Optional[_Iterable[_Union[GitChange, _Mapping]]] = ..., truncated: bool = ..., base_ref: _Optional[str] = ...) -> None: ...

class InspectWorkspaceGitResponse(_message.Message):
    __slots__ = ("snapshot", "diff", "binary", "truncated")
    SNAPSHOT_FIELD_NUMBER: _ClassVar[int]
    DIFF_FIELD_NUMBER: _ClassVar[int]
    BINARY_FIELD_NUMBER: _ClassVar[int]
    TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    snapshot: GitSnapshot
    diff: str
    binary: bool
    truncated: bool
    def __init__(self, snapshot: _Optional[_Union[GitSnapshot, _Mapping]] = ..., diff: _Optional[str] = ..., binary: bool = ..., truncated: bool = ...) -> None: ...
