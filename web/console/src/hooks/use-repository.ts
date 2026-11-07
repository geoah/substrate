/** The repository record, with its name proved where proving it needs nothing
 * from the owner: a name one label under the server's own domain passes the
 * domain check with no DNS record (decision 0208), so a reader who never
 * pressed Check is not told their name is unverified. The check runs once per
 * session and only stamps the record when the server agrees; the host it is
 * guessed from sets whether to ask, never the answer. */

import { useQuery, useQueryClient } from "@tanstack/react-query"

import { verifyDomain } from "@/lib/api/domain"
import { repositoryQueryOptions } from "@/lib/api/repository"
import { underHost } from "@/lib/dns"
import { substrateHostname } from "@/lib/shell"

export function useRepository() {
  const queryClient = useQueryClient()
  const repository = useQuery(repositoryQueryOptions)
  const authority = repository.data?.authority ?? ""
  const ownDomain =
    Boolean(repository.data) &&
    !repository.data?.domainVerifiedAt &&
    underHost(authority, substrateHostname())
  const check = useQuery({
    queryKey: ["repository", "own-domain-check", authority],
    enabled: ownDomain,
    staleTime: Infinity,
    retry: false,
    queryFn: async () => {
      const result = await verifyDomain()
      if (result.verified) {
        await queryClient.invalidateQueries({
          queryKey: repositoryQueryOptions.queryKey,
          exact: true,
        })
      }
      return result
    },
  })
  return {
    repository,
    /** The own-domain check is still deciding: say nothing about the name
     * until it has. */
    checking: ownDomain && check.isPending,
  }
}
