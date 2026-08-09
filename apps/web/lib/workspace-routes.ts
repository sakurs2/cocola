const PROJECT_TASK_PATH = /^\/projects\/[^/]+\/tasks\/[^/]+\/?$/;

export function isProjectTaskPath(pathname: string | null) {
  return Boolean(pathname && PROJECT_TASK_PATH.test(pathname));
}
