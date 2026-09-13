import { describe, expect, it } from 'vitest';
import { MAX_TASK_BYTES, taskByteLength } from './taskInput';

describe('task UTF-8 input limit', () => {
  it.each([
    ['English boundary', 'a'.repeat(20_000), 20_000],
    ['English overflow', 'a'.repeat(20_001), 20_001],
    ['Chinese boundary', '中'.repeat(6_666) + 'ab', 20_000],
    ['Chinese overflow', '中'.repeat(7_000), 21_000],
    ['emoji boundary', '😀'.repeat(5_000), 20_000],
    ['emoji overflow', '😀'.repeat(5_001), 20_004],
    ['trimmed payload', '  中文\n', 6],
    ['blank input', ' \n\t ', 0],
  ])('%s', (_label, input, bytes) => {
    expect(taskByteLength(input)).toBe(bytes);
    expect(taskByteLength(input) <= MAX_TASK_BYTES).toBe(bytes <= 20_000);
  });
});
