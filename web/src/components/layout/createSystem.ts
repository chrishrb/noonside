import { createContext, useContext } from 'react';

/** Opens the shell's "New system" dialog (superadmins). */
export const CreateSystemContext = createContext<() => void>(() => {});
export const useCreateSystem = () => useContext(CreateSystemContext);
