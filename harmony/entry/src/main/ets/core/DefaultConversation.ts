type JsonRecord = Record<string, unknown>;
function text(value: unknown): string { return typeof value === 'string' ? value : ''; }
function record(value: unknown): JsonRecord | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value) ? value as JsonRecord : undefined;
}

/** Main identity and its inventory must come from the same authoritative assistant response. */
export function mainConversationData(response: unknown): JsonRecord | undefined {
  const data = record(response);
  if (!data || !Array.isArray(data.conversations)) return undefined;
  const session = text(data.main), machine = text(data.main_machine_id) || text(data.machine_id);
  if (!session || !machine) return undefined;
  const candidates = data.conversations.map(record).filter((row: JsonRecord | undefined): row is JsonRecord => !!row &&
    (text(row.session_id) || text(row.id)) === session && (text(row.machine_id) || text(data.machine_id)) === machine);
  if (candidates.length !== 1) return undefined;
  return { ...candidates[0], machine_id: machine, session_id: session };
}
