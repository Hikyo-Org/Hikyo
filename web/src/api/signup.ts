import { signupRequestOp, signupVerifyOp, getInstanceMailOp, testInstanceMailOp } from '@hikyo/operations';
import type { SignupRequest, SignupVerifyRequest, InstanceMailTestRequest } from '@hikyo/client';
import { useQuery } from '@tanstack/react-query';

import { ApiError, ok, parsed, transportRefusalText } from './client.ts';

export const requestSignup = (body: SignupRequest) => ok(signupRequestOp, { body });
export const verifySignup = (body: SignupVerifyRequest) => ok(signupVerifyOp, { body });
export const testInstanceMail = (body: InstanceMailTestRequest) => ok(testInstanceMailOp, { body });
export const useInstanceMail = () => useQuery({ queryKey: ['instance-mail'], queryFn: () => parsed(getInstanceMailOp, {}) });

export function signupFailureText(error: unknown): string {
  if (error instanceof ApiError && error.status === 401) return "This link can't be used. Start again from the sign-up page.";
  if (error instanceof ApiError && error.status === 400) return error.detail ?? 'Check your password and organisation name, then try again. Your link is still valid.';
  return transportRefusalText(error) ?? 'The request could not be completed. Try again.';
}
