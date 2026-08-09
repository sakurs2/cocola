from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class InteractionMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    INTERACTION_MODE_UNSPECIFIED: _ClassVar[InteractionMode]
    INTERACTION_MODE_EXECUTE: _ClassVar[InteractionMode]
    INTERACTION_MODE_PLAN: _ClassVar[InteractionMode]

class AgentKnowledgeSourceState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AGENT_KNOWLEDGE_SOURCE_STATE_UNSPECIFIED: _ClassVar[AgentKnowledgeSourceState]
    AGENT_KNOWLEDGE_SOURCE_STATE_READY: _ClassVar[AgentKnowledgeSourceState]
    AGENT_KNOWLEDGE_SOURCE_STATE_TEMPORARILY_UNAVAILABLE: _ClassVar[AgentKnowledgeSourceState]
    AGENT_KNOWLEDGE_SOURCE_STATE_UNAVAILABLE: _ClassVar[AgentKnowledgeSourceState]
INTERACTION_MODE_UNSPECIFIED: InteractionMode
INTERACTION_MODE_EXECUTE: InteractionMode
INTERACTION_MODE_PLAN: InteractionMode
AGENT_KNOWLEDGE_SOURCE_STATE_UNSPECIFIED: AgentKnowledgeSourceState
AGENT_KNOWLEDGE_SOURCE_STATE_READY: AgentKnowledgeSourceState
AGENT_KNOWLEDGE_SOURCE_STATE_TEMPORARILY_UNAVAILABLE: AgentKnowledgeSourceState
AGENT_KNOWLEDGE_SOURCE_STATE_UNAVAILABLE: AgentKnowledgeSourceState

class QueryRequest(_message.Message):
    __slots__ = ("user_id", "session_id", "prompt", "sandbox_id", "max_turns", "attachments", "runtime_id", "skill_id", "allow_workspace_reset", "memory_context", "project_context", "interaction_mode", "require_session_resume", "wiki_references", "agent_context", "agent_knowledge_context", "reasoning_effort")
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
    INTERACTION_MODE_FIELD_NUMBER: _ClassVar[int]
    REQUIRE_SESSION_RESUME_FIELD_NUMBER: _ClassVar[int]
    WIKI_REFERENCES_FIELD_NUMBER: _ClassVar[int]
    AGENT_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    AGENT_KNOWLEDGE_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    REASONING_EFFORT_FIELD_NUMBER: _ClassVar[int]
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
    interaction_mode: InteractionMode
    require_session_resume: bool
    wiki_references: _containers.RepeatedCompositeFieldContainer[WikiReference]
    agent_context: AgentContext
    agent_knowledge_context: AgentKnowledgeContext
    reasoning_effort: str
    def __init__(self, user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., prompt: _Optional[str] = ..., sandbox_id: _Optional[str] = ..., max_turns: _Optional[int] = ..., attachments: _Optional[_Iterable[_Union[Attachment, _Mapping]]] = ..., runtime_id: _Optional[str] = ..., skill_id: _Optional[str] = ..., allow_workspace_reset: bool = ..., memory_context: _Optional[str] = ..., project_context: _Optional[_Union[ProjectContext, _Mapping]] = ..., interaction_mode: _Optional[_Union[InteractionMode, str]] = ..., require_session_resume: bool = ..., wiki_references: _Optional[_Iterable[_Union[WikiReference, _Mapping]]] = ..., agent_context: _Optional[_Union[AgentContext, _Mapping]] = ..., agent_knowledge_context: _Optional[_Union[AgentKnowledgeContext, _Mapping]] = ..., reasoning_effort: _Optional[str] = ...) -> None: ...

class ProjectContext(_message.Message):
    __slots__ = ("project_id", "repository_id", "clone_url", "default_branch", "base_sha", "task_branch", "git_author_name", "git_author_email", "repository_provider", "repository_full_name", "credential_mode", "base_ref")
    PROJECT_ID_FIELD_NUMBER: _ClassVar[int]
    REPOSITORY_ID_FIELD_NUMBER: _ClassVar[int]
    CLONE_URL_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_BRANCH_FIELD_NUMBER: _ClassVar[int]
    BASE_SHA_FIELD_NUMBER: _ClassVar[int]
    TASK_BRANCH_FIELD_NUMBER: _ClassVar[int]
    GIT_AUTHOR_NAME_FIELD_NUMBER: _ClassVar[int]
    GIT_AUTHOR_EMAIL_FIELD_NUMBER: _ClassVar[int]
    REPOSITORY_PROVIDER_FIELD_NUMBER: _ClassVar[int]
    REPOSITORY_FULL_NAME_FIELD_NUMBER: _ClassVar[int]
    CREDENTIAL_MODE_FIELD_NUMBER: _ClassVar[int]
    BASE_REF_FIELD_NUMBER: _ClassVar[int]
    project_id: str
    repository_id: int
    clone_url: str
    default_branch: str
    base_sha: str
    task_branch: str
    git_author_name: str
    git_author_email: str
    repository_provider: str
    repository_full_name: str
    credential_mode: str
    base_ref: str
    def __init__(self, project_id: _Optional[str] = ..., repository_id: _Optional[int] = ..., clone_url: _Optional[str] = ..., default_branch: _Optional[str] = ..., base_sha: _Optional[str] = ..., task_branch: _Optional[str] = ..., git_author_name: _Optional[str] = ..., git_author_email: _Optional[str] = ..., repository_provider: _Optional[str] = ..., repository_full_name: _Optional[str] = ..., credential_mode: _Optional[str] = ..., base_ref: _Optional[str] = ...) -> None: ...

class AgentKnowledgeSource(_message.Message):
    __slots__ = ("type", "label", "url", "node_id")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    LABEL_FIELD_NUMBER: _ClassVar[int]
    URL_FIELD_NUMBER: _ClassVar[int]
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    type: str
    label: str
    url: str
    node_id: str
    def __init__(self, type: _Optional[str] = ..., label: _Optional[str] = ..., url: _Optional[str] = ..., node_id: _Optional[str] = ...) -> None: ...

class AgentKnowledgeEntry(_message.Message):
    __slots__ = ("source_id", "source", "state", "wiki_reference")
    SOURCE_ID_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    WIKI_REFERENCE_FIELD_NUMBER: _ClassVar[int]
    source_id: str
    source: AgentKnowledgeSource
    state: AgentKnowledgeSourceState
    wiki_reference: WikiReference
    def __init__(self, source_id: _Optional[str] = ..., source: _Optional[_Union[AgentKnowledgeSource, _Mapping]] = ..., state: _Optional[_Union[AgentKnowledgeSourceState, str]] = ..., wiki_reference: _Optional[_Union[WikiReference, _Mapping]] = ...) -> None: ...

class AgentKnowledgeContext(_message.Message):
    __slots__ = ("agent_id", "revision", "entries")
    AGENT_ID_FIELD_NUMBER: _ClassVar[int]
    REVISION_FIELD_NUMBER: _ClassVar[int]
    ENTRIES_FIELD_NUMBER: _ClassVar[int]
    agent_id: str
    revision: int
    entries: _containers.RepeatedCompositeFieldContainer[AgentKnowledgeEntry]
    def __init__(self, agent_id: _Optional[str] = ..., revision: _Optional[int] = ..., entries: _Optional[_Iterable[_Union[AgentKnowledgeEntry, _Mapping]]] = ...) -> None: ...

class AgentContext(_message.Message):
    __slots__ = ("id", "version", "name", "instructions", "skill_catalog_ids", "knowledge_sources")
    ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    INSTRUCTIONS_FIELD_NUMBER: _ClassVar[int]
    SKILL_CATALOG_IDS_FIELD_NUMBER: _ClassVar[int]
    KNOWLEDGE_SOURCES_FIELD_NUMBER: _ClassVar[int]
    id: str
    version: int
    name: str
    instructions: str
    skill_catalog_ids: _containers.RepeatedScalarFieldContainer[str]
    knowledge_sources: _containers.RepeatedCompositeFieldContainer[AgentKnowledgeSource]
    def __init__(self, id: _Optional[str] = ..., version: _Optional[int] = ..., name: _Optional[str] = ..., instructions: _Optional[str] = ..., skill_catalog_ids: _Optional[_Iterable[str]] = ..., knowledge_sources: _Optional[_Iterable[_Union[AgentKnowledgeSource, _Mapping]]] = ...) -> None: ...

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

class WikiReference(_message.Message):
    __slots__ = ("node_id", "version_id", "logical_path", "filename", "mime", "oss_key", "size", "sha256")
    NODE_ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_ID_FIELD_NUMBER: _ClassVar[int]
    LOGICAL_PATH_FIELD_NUMBER: _ClassVar[int]
    FILENAME_FIELD_NUMBER: _ClassVar[int]
    MIME_FIELD_NUMBER: _ClassVar[int]
    OSS_KEY_FIELD_NUMBER: _ClassVar[int]
    SIZE_FIELD_NUMBER: _ClassVar[int]
    SHA256_FIELD_NUMBER: _ClassVar[int]
    node_id: str
    version_id: str
    logical_path: str
    filename: str
    mime: str
    oss_key: str
    size: int
    sha256: str
    def __init__(self, node_id: _Optional[str] = ..., version_id: _Optional[str] = ..., logical_path: _Optional[str] = ..., filename: _Optional[str] = ..., mime: _Optional[str] = ..., oss_key: _Optional[str] = ..., size: _Optional[int] = ..., sha256: _Optional[str] = ...) -> None: ...

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
    __slots__ = ("user_id", "session_id", "operation", "path", "diff_target", "project_context", "commit_sha")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    OPERATION_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    DIFF_TARGET_FIELD_NUMBER: _ClassVar[int]
    PROJECT_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    COMMIT_SHA_FIELD_NUMBER: _ClassVar[int]
    user_id: str
    session_id: str
    operation: str
    path: str
    diff_target: str
    project_context: ProjectContext
    commit_sha: str
    def __init__(self, user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., operation: _Optional[str] = ..., path: _Optional[str] = ..., diff_target: _Optional[str] = ..., project_context: _Optional[_Union[ProjectContext, _Mapping]] = ..., commit_sha: _Optional[str] = ...) -> None: ...

class PublishWorkspaceGitRequest(_message.Message):
    __slots__ = ("user_id", "session_id", "project_context", "remote_clone_url", "expected_head_sha")
    USER_ID_FIELD_NUMBER: _ClassVar[int]
    SESSION_ID_FIELD_NUMBER: _ClassVar[int]
    PROJECT_CONTEXT_FIELD_NUMBER: _ClassVar[int]
    REMOTE_CLONE_URL_FIELD_NUMBER: _ClassVar[int]
    EXPECTED_HEAD_SHA_FIELD_NUMBER: _ClassVar[int]
    user_id: str
    session_id: str
    project_context: ProjectContext
    remote_clone_url: str
    expected_head_sha: str
    def __init__(self, user_id: _Optional[str] = ..., session_id: _Optional[str] = ..., project_context: _Optional[_Union[ProjectContext, _Mapping]] = ..., remote_clone_url: _Optional[str] = ..., expected_head_sha: _Optional[str] = ...) -> None: ...

class PublishWorkspaceGitResponse(_message.Message):
    __slots__ = ("head_sha",)
    HEAD_SHA_FIELD_NUMBER: _ClassVar[int]
    head_sha: str
    def __init__(self, head_sha: _Optional[str] = ...) -> None: ...

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
    __slots__ = ("branch", "base_sha", "head_sha", "ahead", "dirty", "changes", "truncated", "base_ref", "commits", "history_truncated", "workspace_revision")
    BRANCH_FIELD_NUMBER: _ClassVar[int]
    BASE_SHA_FIELD_NUMBER: _ClassVar[int]
    HEAD_SHA_FIELD_NUMBER: _ClassVar[int]
    AHEAD_FIELD_NUMBER: _ClassVar[int]
    DIRTY_FIELD_NUMBER: _ClassVar[int]
    CHANGES_FIELD_NUMBER: _ClassVar[int]
    TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    BASE_REF_FIELD_NUMBER: _ClassVar[int]
    COMMITS_FIELD_NUMBER: _ClassVar[int]
    HISTORY_TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    WORKSPACE_REVISION_FIELD_NUMBER: _ClassVar[int]
    branch: str
    base_sha: str
    head_sha: str
    ahead: int
    dirty: bool
    changes: _containers.RepeatedCompositeFieldContainer[GitChange]
    truncated: bool
    base_ref: str
    commits: _containers.RepeatedCompositeFieldContainer[GitCommit]
    history_truncated: bool
    workspace_revision: str
    def __init__(self, branch: _Optional[str] = ..., base_sha: _Optional[str] = ..., head_sha: _Optional[str] = ..., ahead: _Optional[int] = ..., dirty: bool = ..., changes: _Optional[_Iterable[_Union[GitChange, _Mapping]]] = ..., truncated: bool = ..., base_ref: _Optional[str] = ..., commits: _Optional[_Iterable[_Union[GitCommit, _Mapping]]] = ..., history_truncated: bool = ..., workspace_revision: _Optional[str] = ...) -> None: ...

class GitCommit(_message.Message):
    __slots__ = ("sha", "parents", "subject", "author_name", "authored_at", "refs", "files_changed", "additions", "deletions", "body")
    SHA_FIELD_NUMBER: _ClassVar[int]
    PARENTS_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    AUTHOR_NAME_FIELD_NUMBER: _ClassVar[int]
    AUTHORED_AT_FIELD_NUMBER: _ClassVar[int]
    REFS_FIELD_NUMBER: _ClassVar[int]
    FILES_CHANGED_FIELD_NUMBER: _ClassVar[int]
    ADDITIONS_FIELD_NUMBER: _ClassVar[int]
    DELETIONS_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    sha: str
    parents: _containers.RepeatedScalarFieldContainer[str]
    subject: str
    author_name: str
    authored_at: str
    refs: _containers.RepeatedScalarFieldContainer[str]
    files_changed: int
    additions: int
    deletions: int
    body: str
    def __init__(self, sha: _Optional[str] = ..., parents: _Optional[_Iterable[str]] = ..., subject: _Optional[str] = ..., author_name: _Optional[str] = ..., authored_at: _Optional[str] = ..., refs: _Optional[_Iterable[str]] = ..., files_changed: _Optional[int] = ..., additions: _Optional[int] = ..., deletions: _Optional[int] = ..., body: _Optional[str] = ...) -> None: ...

class GitCommitFile(_message.Message):
    __slots__ = ("path", "old_path", "status", "binary", "additions", "deletions")
    PATH_FIELD_NUMBER: _ClassVar[int]
    OLD_PATH_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    BINARY_FIELD_NUMBER: _ClassVar[int]
    ADDITIONS_FIELD_NUMBER: _ClassVar[int]
    DELETIONS_FIELD_NUMBER: _ClassVar[int]
    path: str
    old_path: str
    status: str
    binary: bool
    additions: int
    deletions: int
    def __init__(self, path: _Optional[str] = ..., old_path: _Optional[str] = ..., status: _Optional[str] = ..., binary: bool = ..., additions: _Optional[int] = ..., deletions: _Optional[int] = ...) -> None: ...

class InspectWorkspaceGitResponse(_message.Message):
    __slots__ = ("snapshot", "diff", "binary", "truncated", "commit", "commit_files")
    SNAPSHOT_FIELD_NUMBER: _ClassVar[int]
    DIFF_FIELD_NUMBER: _ClassVar[int]
    BINARY_FIELD_NUMBER: _ClassVar[int]
    TRUNCATED_FIELD_NUMBER: _ClassVar[int]
    COMMIT_FIELD_NUMBER: _ClassVar[int]
    COMMIT_FILES_FIELD_NUMBER: _ClassVar[int]
    snapshot: GitSnapshot
    diff: str
    binary: bool
    truncated: bool
    commit: GitCommit
    commit_files: _containers.RepeatedCompositeFieldContainer[GitCommitFile]
    def __init__(self, snapshot: _Optional[_Union[GitSnapshot, _Mapping]] = ..., diff: _Optional[str] = ..., binary: bool = ..., truncated: bool = ..., commit: _Optional[_Union[GitCommit, _Mapping]] = ..., commit_files: _Optional[_Iterable[_Union[GitCommitFile, _Mapping]]] = ...) -> None: ...
