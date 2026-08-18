import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createContext, type PropsWithChildren, useContext } from 'react';
import { APIError, getCurrentUser, login, logout, type User } from '../api/client';

type LoginInput = { email: string; password: string; remember: boolean };
type AuthContextValue = {
  user: User | null;
  loading: boolean;
  error: Error | null;
  signIn: (input: LoginInput) => Promise<User>;
  signOut: () => Promise<void>;
};

const authKey = ['auth', 'me'] as const;
const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: PropsWithChildren) {
  const queryClient = useQueryClient();
  const authQuery = useQuery({
    queryKey: authKey,
    queryFn: async () => {
      try {
        return await getCurrentUser();
      } catch (error) {
        if (error instanceof APIError && error.status === 401) return null;
        throw error;
      }
    },
    retry: false,
    staleTime: 30_000,
  });
  const loginMutation = useMutation({
    mutationFn: login,
    onSuccess: (user) => queryClient.setQueryData(authKey, user),
  });
  const logoutMutation = useMutation({
    mutationFn: logout,
    onSuccess: () => {
      queryClient.setQueryData(authKey, null);
      queryClient.removeQueries({ queryKey: ['runs'] });
      queryClient.removeQueries({ queryKey: ['sessions'] });
    },
  });

  return (
    <AuthContext.Provider value={{
      user: authQuery.data ?? null,
      loading: authQuery.isPending,
      error: authQuery.error,
      signIn: loginMutation.mutateAsync,
      signOut: logoutMutation.mutateAsync,
    }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error('useAuth must be used inside AuthProvider');
  return value;
}
