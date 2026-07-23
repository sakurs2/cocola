const cacheByOwner = new Map();

function ownerKey(ownerID) {
  return typeof ownerID === "string" ? ownerID.trim() : "";
}

export function readScheduledTaskPageCache(ownerID) {
  const key = ownerKey(ownerID);
  return key ? (cacheByOwner.get(key) ?? null) : null;
}

export function writeScheduledTaskPageCache(ownerID, value) {
  const key = ownerKey(ownerID);
  if (!key) return null;
  const current = cacheByOwner.get(key);
  const hasTasks = Object.prototype.hasOwnProperty.call(value ?? {}, "tasks");
  const hasModels = Object.prototype.hasOwnProperty.call(value ?? {}, "models");
  const cached = {
    tasks: hasTasks
      ? Array.isArray(value?.tasks)
        ? [...value.tasks]
        : []
      : (current?.tasks ?? null),
    models: hasModels
      ? Array.isArray(value?.models)
        ? [...value.models]
        : []
      : (current?.models ?? null),
  };
  cacheByOwner.set(key, cached);
  return cached;
}

export function clearScheduledTaskPageCache(ownerID) {
  const key = ownerKey(ownerID);
  if (key) cacheByOwner.delete(key);
  else cacheByOwner.clear();
}
