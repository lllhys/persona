export interface ReadingAnchor { id: string; offset: number; atEnd: boolean }
export function parseReadingAnchor(value: string): ReadingAnchor {
  try {
    const data = JSON.parse(value) as ReadingAnchor;
    if (data && typeof data.id === 'string' && data.id.length <= 4096 && typeof data.offset === 'number' && Number.isFinite(data.offset) && typeof data.atEnd === 'boolean') {
      return { id: data.id, offset: Math.max(-100000, Math.min(100000, data.offset)), atEnd: data.atEnd };
    }
  } catch (_) {}
  return { id: '', offset: 0, atEnd: true };
}
export function readingAnchorIndex(ids: string[], anchor: ReadingAnchor): number { return anchor.id ? ids.indexOf(anchor.id) : -1; }
