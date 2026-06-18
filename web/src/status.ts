export const statusClass: Record<string, string> = {
  success: "status success",
  failed: "status failed",
  timeout: "status failed",
  canceled: "status muted",
  running: "status running",
  pending: "status pending",
  waiting_confirmation: "status running",
  skipped: "status muted"
};

export function classForStatus(status: string): string {
  return statusClass[status] ?? "status pending";
}

