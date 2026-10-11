import clsx from "clsx";
import type { TextareaHTMLAttributes } from "react";
import { FieldShell, fieldClasses, type FieldChromeProps } from "./Input";
import { callerSetsWidth } from "../lib/fieldWidth";

export type TextareaProps = Omit<
  TextareaHTMLAttributes<HTMLTextAreaElement>,
  "className"
> &
  FieldChromeProps;

export function Textarea({
  label,
  help,
  error,
  mono,
  className,
  ...rest
}: TextareaProps) {
  return (
    <FieldShell label={label} help={help} error={error}>
      <textarea
        {...rest}
        className={clsx(
          fieldClasses({ mono, invalid: Boolean(error), ownWidth: callerSetsWidth(className) }),
          "resize-y",
          className,
        )}
      />
    </FieldShell>
  );
}
