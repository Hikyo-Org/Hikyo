import { z } from 'zod';

/** JSON syntax validation shared by declaration and matrix value forms. */
export const zJSONText = z.string().transform((text, context): unknown => {
  try {
    const parsed: unknown = JSON.parse(text);
    return parsed;
  } catch {
    context.addIssue({ code: 'custom', message: 'Enter valid JSON.' });
    return z.NEVER;
  }
});
