export interface HostedFile {
  id: string;
  path: string;
  sha256: string;
  size_bytes: number;
  created_at: string;
}
export function downloadPath(application: string, path: string) {
  return `/${application}/${path.split("/").map(encodeURIComponent).join("/")}`;
}
