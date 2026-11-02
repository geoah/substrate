/** Settings' Verify domain row (decision 0208): the TXT record that proves the
 * repository's name, to copy into the name's DNS, and a Check that reads it
 * back. A repository named one label under the server's configured public URL
 * passes the check with no record. */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { CopyButton } from "@/components/identity/copy-button"
import { Pill } from "@/components/identity/pill"
import { SettingRow } from "@/components/settings-page/setting-row"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { startDomainVerification, verifyDomain } from "@/lib/api/domain"
import { olderServerMessage } from "@/lib/api/http"
import { repositoryQueryOptions } from "@/lib/api/repository"

function when(at: string): string {
  const d = new Date(at)
  return Number.isNaN(d.getTime()) ? at : d.toLocaleString()
}

export function VerifyDomainRow() {
  const queryClient = useQueryClient()
  const repository = useQuery(repositoryQueryOptions)
  const verifiedAt = repository.data?.domainVerifiedAt ?? ""
  const start = useMutation({ mutationFn: startDomainVerification })
  const check = useMutation({
    mutationFn: verifyDomain,
    onSuccess: (result) => {
      if (result.verified) {
        void queryClient.invalidateQueries({
          queryKey: repositoryQueryOptions.queryKey,
        })
      }
    },
  })
  const proof = start.data

  return (
    <SettingRow
      title={
        <span className="inline-flex flex-wrap items-center gap-2">
          Verify domain
          {verifiedAt ? (
            <Pill tone="ok">Verified</Pill>
          ) : (
            repository.data && <Pill tone="warn">Unverified</Pill>
          )}
        </span>
      }
      description={
        verifiedAt
          ? `Verified ${when(verifiedAt)}. Your repository’s name points at this server and proved it controls the name’s DNS.`
          : "Prove you control your repository’s name: add a TXT record to its DNS, then check it. A name directly under this server’s public address needs no record."
      }
      control={
        <>
          <Button
            variant="outline"
            size="sm"
            disabled={start.isPending}
            onClick={() => start.mutate()}
          >
            {start.isPending && <Spinner />}
            Show record
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={check.isPending}
            onClick={() => check.mutate()}
          >
            {check.isPending && <Spinner />}
            Check
          </Button>
        </>
      }
    >
      {(proof || start.isError || check.data || check.isError) && (
        <div className="flex flex-col gap-3 text-[12.5px]">
          {start.isError && (
            <p role="alert" className="text-destructive">
              The record didn’t load:{" "}
              {olderServerMessage(start.error, "verify a domain") ??
                start.error.message}
            </p>
          )}
          {proof && (
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5">
              <dt className="text-faint">Name</dt>
              <dd className="flex items-start gap-1.5 font-mono [overflow-wrap:anywhere]">
                {proof.name}
                <CopyButton value={proof.name} label="Copy the record name" />
              </dd>
              <dt className="text-faint">Type</dt>
              <dd className="font-mono">TXT</dd>
              <dt className="text-faint">Value</dt>
              <dd className="flex items-start gap-1.5 font-mono [overflow-wrap:anywhere]">
                {proof.record}
                <CopyButton
                  value={proof.record}
                  label="Copy the record value"
                />
              </dd>
            </dl>
          )}
          {check.isError && (
            <p role="alert" className="text-destructive">
              The check didn’t run:{" "}
              {olderServerMessage(check.error, "verify a domain") ??
                check.error.message}
            </p>
          )}
          {check.data &&
            (check.data.verified ? (
              <p role="status" className="text-ok">
                Verified. Your repository’s name is proved.
              </p>
            ) : (
              <p role="alert" className="[overflow-wrap:anywhere]">
                <span className="font-medium">Not verified: </span>
                {check.data.reason}
              </p>
            ))}
        </div>
      )}
    </SettingRow>
  )
}
