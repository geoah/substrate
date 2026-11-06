/** Settings' Verify domain row (decision 0208): the TXT record that proves the
 * repository's name, to copy into the name's DNS, and a Check that reads it
 * back. A repository named one label under the server's configured public URL
 * passes the check with no record. The register page sends a reader who chose
 * their own domain here, to `#verify-domain`. */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { DnsRecord } from "@/components/dns-record"
import { Pill } from "@/components/identity/pill"
import { SettingRow } from "@/components/settings-page/setting-row"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { startDomainVerification, verifyDomain } from "@/lib/api/domain"
import { olderServerMessage } from "@/lib/api/http"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { relativeRecordName, underHost } from "@/lib/dns"
import { substrateHostname } from "@/lib/shell"

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
  const relative = proof ? relativeRecordName(proof.name) : null
  const name = repository.data?.authority ?? "your repository’s name"
  // A guess from the host the console was reached on; the check itself reads
  // only the operator's configured public URL, so this sets the wording and
  // nothing else.
  const onThisServer = underHost(
    repository.data?.authority ?? "",
    substrateHostname()
  )

  return (
    <SettingRow
      id="verify-domain"
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
          ? `Verified ${when(verifiedAt)}: ${name} points at this server.`
          : onThisServer
            ? `${name} is on this server’s domain, so it usually needs no record: just check it.`
            : `Prove you own ${name}: add a DNS record, then check it.`
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
            <ol className="flex list-decimal flex-col gap-3 pl-5 marker:text-faint">
              <li>
                <div className="flex flex-col gap-2">
                  <span>
                    In the DNS settings for{" "}
                    <span className="font-medium [overflow-wrap:anywhere]">
                      {name}
                    </span>
                    , add this TXT record, replacing any already at that name.
                  </span>
                  <DnsRecord
                    name={proof.name}
                    type="TXT"
                    value={proof.record}
                  />
                  {relative && (
                    <span className="text-faint">
                      Some providers add the domain for you. If yours does,
                      enter just{" "}
                      <span className="font-mono [overflow-wrap:anywhere]">
                        {relative.relative}
                      </span>{" "}
                      (your domain being{" "}
                      <span className="font-mono [overflow-wrap:anywhere]">
                        {relative.zone}
                      </span>
                      ).
                    </span>
                  )}
                </div>
              </li>
              <li>Check it. A new record can take a while to appear.</li>
            </ol>
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
                Verified.
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
