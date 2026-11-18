// The folder list under "New agent"'s folder field follows the path in it, filled in or typed (#9452): it lists that
// folder's subfolders, and while a typed path names no folder yet ("~/co"), the folder its last part is in, keeping the
// subfolders whose names start with that part, as a shell completes a path. Only a full path is followed (/…, ~, ~/…,
// C:\…): a relative one names no place the computer would agree on.
export interface TypedFolder {
  /** The folder to list first. */
  path: string;
  /** Where to look when `path` is no folder: the folder its last part is in, and that part. */
  near?: { path: string; prefix: string };
}

export function typedFolder(text: string): TypedFolder | null {
  const path = text.trim();
  if (!(path.startsWith("/") || path === "~" || path.startsWith("~/") || /^[A-Za-z]:[\\/]/.test(path))) return null;
  const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  const prefix = path.slice(cut + 1);
  if (cut < 0 || !prefix) return { path };
  // "/da" is in "/", "C:\da" in "C:\": the root keeps its separator
  const root = cut === 0 || path[cut - 1] === ":";
  return { path, near: { path: path.slice(0, root ? cut + 1 : cut), prefix } };
}

/** The entries whose names start with `prefix`, ignoring case; all of them for none. */
export function startingWith<T extends { name: string }>(entries: T[] | undefined, prefix: string): T[] {
  if (!entries) return [];
  if (!prefix) return entries;
  const p = prefix.toLowerCase();
  return entries.filter((e) => e.name.toLowerCase().startsWith(p));
}
