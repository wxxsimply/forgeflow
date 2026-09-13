export const MAX_TASK_BYTES = 20_000;

export function taskByteLength(task: string): number {
  // Count exactly the trimmed UTF-8 payload that createRun sends to the API.
  return new TextEncoder().encode(task.trim()).byteLength;
}
