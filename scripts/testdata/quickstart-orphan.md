# Orphan child

```sh
bash -c 'sleep 4242 & exit 0' &
sleep 1
echo started
```

```text
started
```
