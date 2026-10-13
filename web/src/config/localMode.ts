// This flag only selects local UI copy. Server-side policy remains authoritative.
export const localControlledDevelopment = import.meta.env.VITE_LOCAL_CONTROLLED_DEVELOPMENT === 'true';
