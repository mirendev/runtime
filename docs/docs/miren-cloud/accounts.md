---
title: Accounts and Sign-In
description: Sign in to Miren Cloud with GitHub or Google, connect more than one sign-in method to the same account, merge accounts you created by accident, and switch between several accounts in one browser.
keywords: [miren cloud, account, sign in, github, google, link, merge, switch account]
---

# Accounts and sign-in

Sign in to [Miren Cloud](https://miren.cloud) with GitHub one day and Google the next, and you don't want two Miren accounts with your organizations on one and your keys on the other. So one Miren account can hold several sign-in methods, and any of them gets you to the same place.

Your Miren account is what belongs to organizations, holds your keys, and shows up as the author of your deploys. GitHub and Google are how you prove it's you.

## Sign-in methods

Your [profile](https://miren.cloud/profile) lists every sign-in method on your account under **Sign-in methods**. You can hold more than one from the same provider, such as a work and a personal Google account.

To add one, choose **Connect GitHub** or **Connect Google** and sign in with that provider. We also need you to have signed in to Miren within the last 15 minutes, and if it's been longer we'll ask you to sign in again first. That way, a laptop left open with your session on it isn't enough for someone to attach their own GitHub or Google to your account.

To remove one, choose **Remove** next to it. We won't remove the last one, since that would lock you out.

We never connect sign-ins just because their email addresses match. Two sign-in methods only end up on the same account when you've just proven you control both.

## What if you already have an account?

When you sign in with a GitHub or Google account we haven't seen before, and its verified email matches a Miren account you already have, we pause before creating a second account and ask what you want:

- **Sign in with the other provider to connect.** Sign in to the account you already have, and we connect the new sign-in method to it. From then on, either one signs you in.
- **Create a separate account.** Keep them apart on purpose. You'll have two accounts with the same name and email, so the account menu shows which sign-in method you're using to tell them apart.

## Merging two accounts

Already have two accounts, say one from GitHub and one from Google? You can fold one into the other. Sign in to the account you want to keep, then connect the other account's sign-in method from your profile. We'll notice that it belongs to another account you control and offer to merge them.

The merge page shows both accounts side by side before anything changes. Confirming moves everything from the other account into the one you're signed in to: its sign-in methods, organization memberships, groups and keys. Its history comes along too, so past deploys and other activity show up under the account you kept. If both accounts were in the same organization, you keep the stronger of the two roles.

:::warning[Merging can't be undone]
The other account stops existing on its own, and removing a sign-in method afterwards won't bring it back: signing in with that method again starts a new, empty account. Look over both accounts on the merge page, and make sure you're keeping the one you meant to, before you confirm.
:::

Anything still signed in as the merged-away account with a token, which is how `miren login` signs in the CLI by default, has to sign in again. Run `miren login` on that machine and approve it as the account you kept (see [Signing in the CLI](#signing-in-the-cli)). A CLI that logged in with `--persistent-key` keeps working, now as the account you kept, since its key moved over with the merge.

## Signing in the CLI

`miren login` signs the CLI in through your browser: it shows a code, and you approve it on Miren Cloud. The CLI gets whichever account the browser is using, and the approval page shows you which one that is. If it's the wrong one, choose **Use another account** before you approve.

The CLI saves each login under a name, called an identity. It's `cloud` unless you pick another. To keep a second account alongside the first, give its login its own name:

```bash
miren login --identity work
```

Each cluster in your CLI config remembers which identity it uses. Add a cluster under a particular one with `miren cluster add --identity work`, move between clusters with [`miren cluster switch`](../command/cluster-switch.md), and run [`miren whoami`](../command/whoami.md) to see which account the current cluster is using. The [`miren login`](../command/login.md) and [`miren cluster add`](../command/cluster-add.md) references cover the rest.

## Several accounts in one browser

Some people keep two accounts on purpose, one for work and one for side projects. Choose **Add another account** from the account menu and sign in, and the browser stays signed in to both. The others show up in the account menu under **Also signed in**. Choose one to switch to it without signing in again.

A browser holds up to five accounts. You can switch to one for up to seven days after you last signed in to it. After that, we'll ask you to sign in again.

**Sign out** signs the browser out of every account at once. That's the trade-off for staying signed in: on a shared computer, anyone who signs in on that browser after you can switch into your accounts until you sign out.
