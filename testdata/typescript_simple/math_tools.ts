/**
 * Add two numbers together.
 */
export function addNumbers(a: number, b: number): number {
  return a + b;
}

/**
 * Reverse a string.
 */
export function reverseString(text: string): string {
  return text.split('').reverse().join('');
}

// Private helper - should not be exported
function _helper(): void {}
